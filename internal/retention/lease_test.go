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

// TestLeaseProtectsFromDelete covers T2.8: the janitor must not prune a segment
// overlapped by an unexpired lease (prune-during-lease → footage survives), and
// must prune once the only lease has expired (expiry frees it) or does not
// overlap. Uses disjoint windows so each scenario's lease is independent.
func TestLeaseProtectsFromDelete(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	j := New(&config.Config{}, st, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := st.CreateCamera(&store.Camera{ID: "cam1", Name: "cam1"}); err != nil {
		t.Fatal(err)
	}
	mkSeg := func(id string, start, end int64) *store.Segment {
		f := filepath.Join(t.TempDir(), id+".ts")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		seg := &store.Segment{ID: id, CameraID: "cam1", Path: f, StartTS: start, EndTS: end, SizeBytes: 1}
		if err := st.InsertSegment(seg); err != nil {
			t.Fatal(err)
		}
		return seg
	}
	now := time.Now().UnixMilli()

	// 1. Unexpired overlapping lease ⇒ protected.
	seg1 := mkSeg("s1", 1000, 2000)
	if _, err := st.CreateLease("cam1", 1500, 5000, now+3_600_000, "export:e1"); err != nil {
		t.Fatal(err)
	}
	j.leasesActive = true
	if j.delete(seg1) {
		t.Fatal("delete() pruned a segment under an active lease")
	}
	if _, err := st.SegmentByID("s1"); err != nil {
		t.Errorf("segment row removed despite the lease: %v", err)
	}

	// 2. Expired lease ⇒ no protection (expiry frees it) even with a stale
	// leasesActive snapshot — the overlap query itself respects expiry.
	seg2 := mkSeg("s2", 20_000, 21_000)
	if _, err := st.CreateLease("cam1", 20_500, 25_000, now-1000, "export:old"); err != nil {
		t.Fatal(err)
	}
	j.leasesActive = true
	if !j.delete(seg2) {
		t.Fatal("delete() skipped a segment whose only lease has expired")
	}

	// 3. Unexpired but non-overlapping lease ⇒ no protection.
	seg3 := mkSeg("s3", 30_000, 31_000)
	if _, err := st.CreateLease("cam1", 39_000, 40_000, now+3_600_000, "x"); err != nil {
		t.Fatal(err)
	}
	j.leasesActive = true
	if !j.delete(seg3) {
		t.Fatal("delete() skipped a segment no lease overlaps")
	}
}
