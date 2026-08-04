package store

import (
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestStorageLocationCRUD(t *testing.T) {
	st := testStore(t)

	// Empty to start.
	if locs, err := st.ListStorageLocations(); err != nil || len(locs) != 0 {
		t.Fatalf("expected 0 locations, got %d (err=%v)", len(locs), err)
	}

	loc := &StorageLocation{
		Name:            "NAS one",
		Type:            LocationLocal,
		Config:          map[string]any{"path": "/mnt/nas1"},
		AllocationBytes: 500 << 30,
		Enabled:         true,
	}
	if err := st.CreateStorageLocation(loc); err != nil {
		t.Fatalf("create: %v", err)
	}
	if loc.ID == "" {
		t.Fatal("expected generated id")
	}

	got, err := st.GetStorageLocation(loc.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "NAS one" || got.Type != LocationLocal {
		t.Fatalf("unexpected: %+v", got)
	}
	if p, _ := got.Config["path"].(string); p != "/mnt/nas1" {
		t.Fatalf("config path not round-tripped: %q", p)
	}
	if got.AllocationBytes != 500<<30 {
		t.Fatalf("allocation not round-tripped: %d", got.AllocationBytes)
	}
	if got.HasCredential {
		t.Fatal("expected no credential")
	}

	// Secret round-trips only via the dedicated accessor and flips HasCredential.
	if err := st.SetLocationSecret(loc.ID, "cipher-text"); err != nil {
		t.Fatalf("set secret: %v", err)
	}
	if enc, err := st.LocationSecret(loc.ID); err != nil || enc != "cipher-text" {
		t.Fatalf("get secret: %q err=%v", enc, err)
	}
	got, _ = st.GetStorageLocation(loc.ID)
	if !got.HasCredential {
		t.Fatal("expected HasCredential after SetLocationSecret")
	}

	// Update must not wipe the secret.
	got.Name = "NAS renamed"
	got.AllocationBytes = 1 << 40
	if err := st.UpdateStorageLocation(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, _ := st.GetStorageLocation(loc.ID)
	if after.Name != "NAS renamed" || after.AllocationBytes != 1<<40 {
		t.Fatalf("update not applied: %+v", after)
	}
	if !after.HasCredential {
		t.Fatal("update wiped the credential")
	}

	if err := st.DeleteStorageLocation(loc.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.GetStorageLocation(loc.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestPerLocationSegmentQueries(t *testing.T) {
	st := testStore(t)

	loc := &StorageLocation{Name: "L", Type: LocationLocal, Config: map[string]any{"path": "/mnt/l"}, Enabled: true}
	if err := st.CreateStorageLocation(loc); err != nil {
		t.Fatalf("create loc: %v", err)
	}

	// Segments FK to cameras, so create them first.
	for _, id := range []string{"camA", "camB"} {
		if err := st.CreateCamera(&Camera{ID: id, Name: id, RTSPMainURL: "rtsp://x", RecordMode: ModeContinuous}); err != nil {
			t.Fatalf("create camera %s: %v", id, err)
		}
	}

	// Two segments on default, one on the configured location.
	segs := []*Segment{
		{CameraID: "camA", LocationID: DefaultLocationID, Path: "/rec/camA/1.ts", StartTS: 1000, EndTS: 11000, DurationMS: 10000, SizeBytes: 100},
		{CameraID: "camA", LocationID: DefaultLocationID, Path: "/rec/camA/2.ts", StartTS: 12000, EndTS: 22000, DurationMS: 10000, SizeBytes: 200},
		{CameraID: "camB", LocationID: loc.ID, Path: "camB/3.ts", StartTS: 2000, EndTS: 12000, DurationMS: 10000, SizeBytes: 400},
	}
	for _, s := range segs {
		if err := st.InsertSegment(s); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	if b, err := st.TotalBytesInLocation(DefaultLocationID); err != nil || b != 300 {
		t.Fatalf("default bytes = %d err=%v (want 300)", b, err)
	}
	if b, err := st.TotalBytesInLocation(loc.ID); err != nil || b != 400 {
		t.Fatalf("loc bytes = %d err=%v (want 400)", b, err)
	}
	if n, err := st.CountSegmentsInLocation(loc.ID); err != nil || n != 1 {
		t.Fatalf("loc count = %d err=%v (want 1)", n, err)
	}

	// Oldest-in-location honors the physical location and start order.
	oldest, err := st.OldestSegmentsInLocation(DefaultLocationID, 10)
	if err != nil || len(oldest) != 2 {
		t.Fatalf("oldest default = %d err=%v (want 2)", len(oldest), err)
	}
	if oldest[0].StartTS != 1000 {
		t.Fatalf("oldest default not start-ordered: %d", oldest[0].StartTS)
	}

	// A round-tripped segment carries its location id.
	byID, err := st.SegmentByID(segs[2].ID)
	if err != nil || byID.LocationID != loc.ID {
		t.Fatalf("segment location id not persisted: %+v err=%v", byID, err)
	}
}

func TestSetCameraLocationAndCount(t *testing.T) {
	st := testStore(t)

	loc := &StorageLocation{Name: "L", Type: LocationLocal, Config: map[string]any{"path": "/mnt/l"}, Enabled: true}
	if err := st.CreateStorageLocation(loc); err != nil {
		t.Fatalf("create loc: %v", err)
	}
	cam := &Camera{Name: "cam", RTSPMainURL: "rtsp://x", RecordMode: ModeContinuous}
	if err := st.CreateCamera(cam); err != nil {
		t.Fatalf("create camera: %v", err)
	}
	// New camera defaults to the default location.
	if cam.LocationID != DefaultLocationID {
		t.Fatalf("expected default location, got %q", cam.LocationID)
	}
	if n, _ := st.CamerasUsingLocation(loc.ID); n != 0 {
		t.Fatalf("expected 0 cameras on loc, got %d", n)
	}

	if err := st.SetCameraLocation(cam.ID, loc.ID); err != nil {
		t.Fatalf("set camera location: %v", err)
	}
	got, _ := st.GetCamera(cam.ID)
	if got.LocationID != loc.ID {
		t.Fatalf("camera location not updated: %q", got.LocationID)
	}
	if n, _ := st.CamerasUsingLocation(loc.ID); n != 1 {
		t.Fatalf("expected 1 camera on loc, got %d", n)
	}
	if n, _ := st.CamerasUsingLocation(DefaultLocationID); n != 0 {
		t.Fatalf("expected 0 cameras on default, got %d", n)
	}
}
