package api

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

// TestPurgeCameraFilesReclaimsDisk covers T0.2: deleting a camera must reclaim
// its on-disk footage and thumbnails, not just cascade the DB rows. It exercises
// purgeCameraFiles directly (the full handler spins the recorder, nil here) with
// a local, absolute-path segment and an event thumbnail. Self-contained so it
// needs only a store-backed Server.
func TestPurgeCameraFilesReclaimsDisk(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "purge.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := &Server{
		st:  st,
		cfg: &config.Config{DataDir: dir, RecordingsDir: filepath.Join(dir, "recordings")},
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if err := st.CreateCamera(&store.Camera{ID: "camX", Name: "Doomed"}); err != nil {
		t.Fatalf("create camera: %v", err)
	}

	// A real segment file on disk + its index row (local ⇒ absolute path).
	segFile := filepath.Join(t.TempDir(), "seg.ts")
	if err := os.WriteFile(segFile, []byte("fake ts bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertSegment(&store.Segment{
		ID: "seg1", CameraID: "camX", Path: segFile,
		StartTS: 1000, EndTS: 11000, DurationMS: 10000, SizeBytes: 13,
	}); err != nil {
		t.Fatalf("insert segment: %v", err)
	}

	// An event with a thumbnail file.
	thumb := filepath.Join(t.TempDir(), "thumb.jpg")
	if err := os.WriteFile(thumb, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertEvent(&store.Event{
		ID: "ev1", CameraID: "camX", Type: "motion", StartTS: 2000, EndTS: 3000, ThumbPath: thumb,
	}); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	s.purgeCameraFiles("camX")

	if _, err := os.Stat(segFile); !os.IsNotExist(err) {
		t.Errorf("segment file still present after purge (err=%v) — footage would orphan", err)
	}
	if _, err := os.Stat(thumb); !os.IsNotExist(err) {
		t.Errorf("event thumbnail still present after purge (err=%v)", err)
	}
}
