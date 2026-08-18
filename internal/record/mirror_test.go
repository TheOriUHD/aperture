package record

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

// TestMirrorOnceCopiesToLocalDestination pins the mirror fan-out: a completed,
// indexed segment on the primary gets copied to a local mirror destination with a
// copy row, idempotently, and without ever touching the ffmpeg path.
func TestMirrorOnceCopiesToLocalDestination(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	r := New(&config.Config{}, st, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "c1", RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	mirrorDir := t.TempDir()
	loc := &store.StorageLocation{Name: "Second disk", Type: store.LocationLocal,
		Config: map[string]any{"path": mirrorDir}, Enabled: true}
	if err := st.CreateStorageLocation(loc); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCameraDestinations("c1", "", []store.CameraDestination{
		{CameraID: "c1", LocationID: ""},
		{CameraID: "c1", LocationID: loc.ID},
	}); err != nil {
		t.Fatal(err)
	}

	// A completed segment on the primary, backed by a real file.
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "1000.ts")
	if err := os.WriteFile(src, []byte("segment-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UnixMilli() - 60_000
	seg := &store.Segment{ID: store.NewID(), CameraID: "c1", LocationID: "",
		Path: src, StartTS: start, EndTS: start + 10_000, DurationMS: 10_000, SizeBytes: 13}
	if err := st.InsertSegment(seg); err != nil {
		t.Fatal(err)
	}

	r.mirrorOnce()

	copies, err := st.CopiesForSegment(seg.ID)
	if err != nil || len(copies) != 1 {
		t.Fatalf("want 1 copy row, got %d err=%v", len(copies), err)
	}
	c := copies[0]
	if c.LocationID != loc.ID {
		t.Errorf("copy on wrong location: %q", c.LocationID)
	}
	data, err := os.ReadFile(c.Path)
	if err != nil {
		t.Fatalf("mirror file unreadable: %v", err)
	}
	if string(data) != "segment-bytes" {
		t.Errorf("mirror content differs from the canonical copy")
	}
	// Copies use the same epoch-second naming as recorded segments.
	if got := filepath.Join(mirrorDir, "c1", strconv.FormatInt(seg.StartTS/1000, 10)+".ts"); c.Path != got {
		t.Errorf("mirror path = %q, want %q", c.Path, got)
	}

	// Idempotent: a second pass copies nothing new.
	r.mirrorOnce()
	if copies, _ := st.CopiesForSegment(seg.ID); len(copies) != 1 {
		t.Errorf("second pass must not duplicate the copy")
	}

	// The original is untouched.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("canonical file must be untouched: %v", err)
	}
}

// TestMirrorRespectsRetentionHorizon pins that footage older than the mirror's
// retention is never copied — transferring bytes the destination would
// immediately age out is pure waste.
func TestMirrorRespectsRetentionHorizon(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	r := New(&config.Config{}, st, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "c1", RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	mirrorDir := t.TempDir()
	loc := &store.StorageLocation{Name: "M", Type: store.LocationLocal,
		Config: map[string]any{"path": mirrorDir}, Enabled: true}
	if err := st.CreateStorageLocation(loc); err != nil {
		t.Fatal(err)
	}
	// The mirror keeps only 1 day.
	if err := st.SetCameraDestinations("c1", "", []store.CameraDestination{
		{CameraID: "c1", LocationID: ""},
		{CameraID: "c1", LocationID: loc.ID, RetentionDays: 1},
	}); err != nil {
		t.Fatal(err)
	}
	srcDir := t.TempDir()
	old := filepath.Join(srcDir, "old.ts")
	if err := os.WriteFile(old, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	seg := &store.Segment{ID: store.NewID(), CameraID: "c1", LocationID: "",
		Path: old, StartTS: time.Now().UnixMilli() - 2*86_400_000, EndTS: time.Now().UnixMilli() - 2*86_400_000 + 10_000,
		DurationMS: 10_000, SizeBytes: 1}
	if err := st.InsertSegment(seg); err != nil {
		t.Fatal(err)
	}

	r.mirrorOnce()

	if copies, _ := st.CopiesForSegment(seg.ID); len(copies) != 0 {
		t.Errorf("2d-old footage must not be copied to a 1d-retention mirror")
	}
}
