package retention

import (
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
