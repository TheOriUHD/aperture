package retention

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

func TestFreeFloor(t *testing.T) {
	const gib = int64(1) << 30
	cases := []struct {
		name  string
		total int64
		want  int64
	}{
		{"small filesystem: a quarter, not an unreachable 5 GiB", 2 * gib, 2 * gib / 4},
		{"mid-size filesystem keeps the 5 GiB floor", 32 * gib, 5 * gib},
		{"large filesystem keeps 7%", 1000 * gib, int64(float64(1000*gib) * minFreeFraction)},
	}
	for _, c := range cases {
		if got := freeFloor(c.total); got != c.want {
			t.Errorf("%s: freeFloor(%d) = %d, want %d", c.name, c.total, got, c.want)
		}
	}
}

// TestFreeSpaceNetSparesSmallFilesystems pins that an unallocated local location on
// a filesystem smaller than the 5 GiB floor keeps its footage while it has room.
// The floor used to be unreachable there, so every pass deleted every segment on it.
func TestFreeSpaceNetSparesSmallFilesystems(t *testing.T) {
	const gib = int64(1) << 30
	for _, c := range []struct {
		name        string
		free        int64
		wantSurvive bool
	}{
		{"mostly empty 2 GiB stick", 19 * gib / 10, true},
		{"genuinely full 2 GiB stick", gib / 10, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			j, st, _ := mirrorTestSetup(t)
			free := c.free
			j.du = func(string) (int64, int64, bool) { return 2 * gib, free, true }
			if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "c1", RetentionDays: 7}); err != nil {
				t.Fatal(err)
			}
			stick := &store.StorageLocation{Name: "USB", Type: store.LocationLocal,
				Config: map[string]any{"path": t.TempDir()}, Enabled: true}
			if err := st.CreateStorageLocation(stick); err != nil {
				t.Fatal(err)
			}
			seg := fileSeg(t, st, stick.Config["path"].(string), "c1", stick.ID, time.Now().UnixMilli()-3_600_000)

			j.enforce()

			_, err := st.SegmentByID(seg.ID)
			if survived := err == nil; survived != c.wantSurvive {
				t.Fatalf("segment survived=%v, want %v", survived, c.wantSurvive)
			}
		})
	}
}

// TestFreeSpaceNetPromotesMirroredFootage pins that a full default disk never
// destroys footage a NAS mirror still holds: the free-space net promotes the
// mirror copy and frees only the local file. Footage without a mirror is still
// deleted, so a full disk is still relieved.
func TestFreeSpaceNetPromotesMirroredFootage(t *testing.T) {
	j, st, dir := mirrorTestSetup(t)
	nasDir := t.TempDir()
	// The default disk is full; the NAS has room.
	j.du = func(path string) (int64, int64, bool) {
		if path == nasDir {
			return 1 << 40, 1 << 39, true
		}
		return 1 << 40, 0, true
	}
	if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "c1", RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	nas := &store.StorageLocation{Name: "NAS", Type: store.LocationLocal,
		Config: map[string]any{"path": nasDir}, Enabled: true}
	if err := st.CreateStorageLocation(nas); err != nil {
		t.Fatal(err)
	}
	hourAgo := time.Now().UnixMilli() - 3_600_000
	mirrored := fileSeg(t, st, dir, "c1", "", hourAgo)
	copyPath := filepath.Join(nasDir, "copy.ts")
	if err := os.WriteFile(copyPath, []byte("xx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertSegmentCopy(&store.SegmentCopy{SegmentID: mirrored.ID, LocationID: nas.ID, Path: copyPath, SizeBytes: 2}); err != nil {
		t.Fatal(err)
	}
	unmirrored := fileSeg(t, st, dir, "c1", "", hourAgo+10_000)

	j.enforce()

	got, err := st.SegmentByID(mirrored.ID)
	if err != nil {
		t.Fatalf("mirrored footage was destroyed by the free-space net: %v", err)
	}
	if got.LocationID != nas.ID || got.Path != copyPath {
		t.Fatalf("segment should have been promoted to its NAS copy: %+v", got)
	}
	if _, err := os.Stat(mirrored.Path); !os.IsNotExist(err) {
		t.Error("the local file should be freed after promotion")
	}
	if _, err := os.Stat(copyPath); err != nil {
		t.Errorf("the NAS copy must survive: %v", err)
	}
	if _, err := st.SegmentByID(unmirrored.ID); err == nil {
		t.Error("footage without a mirror on the full disk should still be deleted")
	}
}

// TestFreeSpaceNetFreesMirrorCopiesFirst pins that when a location runs low, the
// redundant mirror copies parked there are freed before any canonical footage on
// it, even older footage.
func TestFreeSpaceNetFreesMirrorCopiesFirst(t *testing.T) {
	j, st, dir := mirrorTestSetup(t)
	usbDir := t.TempDir()
	copyPath := filepath.Join(usbDir, "copy.ts")
	// The stick reads full until the mirror copy is gone; freeing it is enough.
	j.du = func(path string) (int64, int64, bool) {
		if path == usbDir {
			if _, err := os.Stat(copyPath); err == nil {
				return 1 << 40, 0, true
			}
		}
		return 1 << 40, 1 << 39, true
	}
	if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "c1", RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	usb := &store.StorageLocation{Name: "USB", Type: store.LocationLocal,
		Config: map[string]any{"path": usbDir}, Enabled: true}
	if err := st.CreateStorageLocation(usb); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	// Older canonical footage on the stick, its only copy...
	onStick := fileSeg(t, st, usbDir, "c1", usb.ID, now-2*3_600_000)
	// ...and a newer segment recorded on the default disk, mirrored onto the stick.
	elsewhere := fileSeg(t, st, dir, "c1", "", now-3_600_000)
	if err := os.WriteFile(copyPath, []byte("xx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertSegmentCopy(&store.SegmentCopy{SegmentID: elsewhere.ID, LocationID: usb.ID, Path: copyPath, SizeBytes: 2}); err != nil {
		t.Fatal(err)
	}

	j.enforce()

	if copies, _ := st.CopiesForSegment(elsewhere.ID); len(copies) != 0 {
		t.Errorf("the redundant mirror copy should have been freed: %+v", copies)
	}
	if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
		t.Error("the mirror copy's file should be gone")
	}
	if _, err := st.SegmentByID(onStick.ID); err != nil {
		t.Fatalf("canonical footage on the stick was evicted although freeing the copy was enough: %v", err)
	}
	if _, err := os.Stat(onStick.Path); err != nil {
		t.Errorf("its file must survive: %v", err)
	}
	if _, err := st.SegmentByID(elsewhere.ID); err != nil {
		t.Errorf("the mirrored segment's canonical copy must be untouched: %v", err)
	}
}
