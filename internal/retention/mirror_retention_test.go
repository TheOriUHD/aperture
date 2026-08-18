package retention

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

func mirrorTestSetup(t *testing.T) (*Janitor, *store.Store, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	j := New(&config.Config{}, st, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return j, st, t.TempDir()
}

func fileSeg(t *testing.T, st *store.Store, dir, camID, locID string, start int64) *store.Segment {
	t.Helper()
	p := filepath.Join(dir, store.NewID()+".ts")
	if err := os.WriteFile(p, []byte("xx"), 0o644); err != nil {
		t.Fatal(err)
	}
	seg := &store.Segment{ID: store.NewID(), CameraID: camID, LocationID: locID,
		Path: p, StartTS: start, EndTS: start + 10_000, DurationMS: 10_000, SizeBytes: 2}
	if err := st.InsertSegment(seg); err != nil {
		t.Fatal(err)
	}
	return seg
}

// TestDestinationRetentionPromotes pins the "local keeps 1 day, NAS keeps 7"
// behavior: when the canonical copy on the short-retention location expires but a
// mirror copy survives, the mirror is PROMOTED — the local file goes, the
// footage does not.
func TestDestinationRetentionPromotes(t *testing.T) {
	j, st, dir := mirrorTestSetup(t)
	if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "c1", RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	nasDir := t.TempDir()
	nas := &store.StorageLocation{Name: "NAS", Type: store.LocationLocal,
		Config: map[string]any{"path": nasDir}, Enabled: true}
	if err := st.CreateStorageLocation(nas); err != nil {
		t.Fatal(err)
	}
	// Local (default) keeps 1 day; the NAS mirror keeps the camera's 7.
	if err := st.SetCameraDestinations("c1", "", []store.CameraDestination{
		{CameraID: "c1", LocationID: "", RetentionDays: 1},
		{CameraID: "c1", LocationID: nas.ID},
	}); err != nil {
		t.Fatal(err)
	}

	// A 2-day-old segment: canonical on local, mirror copy on the NAS.
	old := time.Now().UnixMilli() - 2*86_400_000
	seg := fileSeg(t, st, dir, "c1", "", old)
	copyPath := filepath.Join(nasDir, "copy.ts")
	if err := os.WriteFile(copyPath, []byte("xx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertSegmentCopy(&store.SegmentCopy{SegmentID: seg.ID, LocationID: nas.ID, Path: copyPath, SizeBytes: 2}); err != nil {
		t.Fatal(err)
	}
	// A fresh segment stays put entirely.
	fresh := fileSeg(t, st, dir, "c1", "", time.Now().UnixMilli()-3_600_000)

	j.enforce()

	// The old segment survived — promoted to the NAS copy — and its local file is gone.
	got, err := st.SegmentByID(seg.ID)
	if err != nil {
		t.Fatalf("promoted segment must survive: %v", err)
	}
	if got.LocationID != nas.ID || got.Path != copyPath {
		t.Fatalf("segment should now live on the NAS: %+v", got)
	}
	if _, err := os.Stat(seg.Path); !os.IsNotExist(err) {
		t.Errorf("old local file should be removed after promotion")
	}
	if _, err := os.Stat(copyPath); err != nil {
		t.Errorf("NAS copy must still exist: %v", err)
	}
	if copies, _ := st.CopiesForSegment(seg.ID); len(copies) != 0 {
		t.Errorf("copy row should be consumed by promotion")
	}
	// The fresh segment is untouched.
	if _, err := st.SegmentByID(fresh.ID); err != nil {
		t.Errorf("fresh segment must survive: %v", err)
	}
}

// TestDestinationRetentionKeepsLastCopy pins the safety rule: a canonical copy
// past its destination's retention with NO surviving mirror is kept, never
// destroyed — another destination still wants it and the mirror pass simply
// hasn't copied it yet.
func TestDestinationRetentionKeepsLastCopy(t *testing.T) {
	j, st, dir := mirrorTestSetup(t)
	if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "c1", RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	nas := &store.StorageLocation{Name: "NAS", Type: store.LocationSMB, Enabled: true}
	if err := st.CreateStorageLocation(nas); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCameraDestinations("c1", "", []store.CameraDestination{
		{CameraID: "c1", LocationID: "", RetentionDays: 1},
		{CameraID: "c1", LocationID: nas.ID},
	}); err != nil {
		t.Fatal(err)
	}
	seg := fileSeg(t, st, dir, "c1", "", time.Now().UnixMilli()-2*86_400_000)

	j.enforce()

	if _, err := st.SegmentByID(seg.ID); err != nil {
		t.Fatalf("the ONLY copy must never be destroyed by per-destination retention: %v", err)
	}
	if _, err := os.Stat(seg.Path); err != nil {
		t.Errorf("its file must survive too: %v", err)
	}
}

// TestBlanketAgeUsesMaxRetention pins that footage dies everywhere only past the
// LONGEST destination retention — and that a full delete removes mirror copies.
func TestBlanketAgeUsesMaxRetention(t *testing.T) {
	j, st, dir := mirrorTestSetup(t)
	if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "c1", RetentionDays: 2}); err != nil {
		t.Fatal(err)
	}
	nasDir := t.TempDir()
	nas := &store.StorageLocation{Name: "NAS", Type: store.LocationLocal,
		Config: map[string]any{"path": nasDir}, Enabled: true}
	if err := st.CreateStorageLocation(nas); err != nil {
		t.Fatal(err)
	}
	// Camera retention 2d; the NAS overrides to 5d ⇒ nothing dies before 5d.
	if err := st.SetCameraDestinations("c1", "", []store.CameraDestination{
		{CameraID: "c1", LocationID: ""},
		{CameraID: "c1", LocationID: nas.ID, RetentionDays: 5},
	}); err != nil {
		t.Fatal(err)
	}

	seg3d := fileSeg(t, st, dir, "c1", "", time.Now().UnixMilli()-3*86_400_000)
	seg6d := fileSeg(t, st, dir, "c1", "", time.Now().UnixMilli()-6*86_400_000)
	c6 := filepath.Join(nasDir, "c6.ts")
	if err := os.WriteFile(c6, []byte("xx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertSegmentCopy(&store.SegmentCopy{SegmentID: seg6d.ID, LocationID: nas.ID, Path: c6, SizeBytes: 2}); err != nil {
		t.Fatal(err)
	}

	j.enforce()

	// 6d-old: past every retention → dead everywhere, mirror file included.
	if _, err := st.SegmentByID(seg6d.ID); err == nil {
		t.Errorf("6d-old segment should be fully deleted")
	}
	if _, err := os.Stat(c6); !os.IsNotExist(err) {
		t.Errorf("its NAS mirror file should be deleted with it")
	}
	// 3d-old: past the camera's 2d but inside the NAS's 5d → the ROW survives
	// (promoted or pending mirror), never silently gone.
	if _, err := st.SegmentByID(seg3d.ID); err != nil {
		t.Errorf("3d-old segment must survive (NAS still wants it): %v", err)
	}
}
