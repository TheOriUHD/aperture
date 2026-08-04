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

// TestExportLockedSkipsDelete covers T0.5: the janitor must not prune a segment
// that overlaps the window of a queued/running export (data-loss: pruning
// footage a render is mid-read of). A non-matching export leaves deletion free.
func TestExportLockedSkipsDelete(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	j := New(&config.Config{}, st, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// The camera must exist first — segments FK-reference cameras.
	if err := st.CreateCamera(&store.Camera{ID: "cam1", Name: "cam1"}); err != nil {
		t.Fatal(err)
	}

	// A segment for cam1 covering [1000,2000] ms, backed by a real file.
	f := filepath.Join(t.TempDir(), "s.ts")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	seg := &store.Segment{ID: "s1", CameraID: "cam1", Path: f, StartTS: 1000, EndTS: 2000, SizeBytes: 1}
	if err := st.InsertSegment(seg); err != nil {
		t.Fatal(err)
	}

	// Active export for cam1 overlapping the segment ⇒ delete is a no-op.
	j.activeExports = []*store.ExportJob{{ID: "e1", CameraID: "cam1", FromTS: 1500, ToTS: 5000, Status: "running"}}
	if j.delete(seg) {
		t.Fatal("delete() reported progress on an export-locked segment")
	}
	if _, err := st.SegmentByID("s1"); err != nil {
		t.Errorf("segment row was removed despite the export lock: %v", err)
	}
	if _, err := os.Stat(f); err != nil {
		t.Errorf("segment file was removed despite the export lock: %v", err)
	}

	// Export on a different camera ⇒ no protection, delete proceeds.
	j.activeExports = []*store.ExportJob{{ID: "e2", CameraID: "other", FromTS: 1500, ToTS: 5000, Status: "running"}}
	if !j.delete(seg) {
		t.Fatal("delete() skipped a segment no active export covers")
	}
	if _, err := st.SegmentByID("s1"); err == nil {
		t.Errorf("segment row should be gone after an unprotected delete")
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("segment file should be removed after an unprotected delete")
	}
}

// TestExportProtectionAgeBound covers the T0.5 bound: a job that has sat past
// exportMaxProtectAge (a render with no per-job timeout, likely stuck) stops
// protecting its footage, so it can't hold segments on a near-full disk for the
// full 24h export TTL.
func TestExportProtectionAgeBound(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	j := New(&config.Config{}, st, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := st.CreateCamera(&store.Camera{ID: "cam1", Name: "cam1"}); err != nil {
		t.Fatal(err)
	}
	nowSec := time.Now().Unix()
	// A fresh render and one older than the protection bound (both "running").
	mustExport := func(id string, createdAt int64) {
		if err := st.CreateExport(&store.ExportJob{
			ID: id, CameraID: "cam1", FromTS: 1000, ToTS: 2000, Status: "running", CreatedAt: createdAt,
		}); err != nil {
			t.Fatalf("create export %s: %v", id, err)
		}
	}
	mustExport("fresh", nowSec)
	mustExport("stale", nowSec-int64((exportMaxProtectAge+time.Hour)/time.Second))

	fresh := j.freshExports()
	if len(fresh) != 1 || fresh[0].ID != "fresh" {
		ids := make([]string, len(fresh))
		for i, e := range fresh {
			ids[i] = e.ID
		}
		t.Fatalf("expected only the fresh export to protect footage, got %v", ids)
	}
}
