package store

import "testing"

func seedCam(t *testing.T, s *Store, id, name, locID string) {
	t.Helper()
	if err := s.CreateCamera(&Camera{ID: id, Name: name, LocationID: locID, RetentionDays: 7}); err != nil {
		t.Fatalf("create camera: %v", err)
	}
	// Mirror the migration's seed: the camera's current location is its membership.
	if err := s.SetCameraDestinations(id, locID, []CameraDestination{{CameraID: id, LocationID: locID}}); err != nil {
		t.Fatalf("seed destination: %v", err)
	}
}

func seedSeg(t *testing.T, s *Store, camID, locID string, start, size int64) *Segment {
	t.Helper()
	seg := &Segment{ID: NewID(), CameraID: camID, LocationID: locID,
		Path: "/x/" + NewID() + ".ts", StartTS: start, EndTS: start + 10_000, DurationMS: 10_000, SizeBytes: size}
	if err := s.InsertSegment(seg); err != nil {
		t.Fatalf("insert segment: %v", err)
	}
	return seg
}

// TestCameraDestinations pins the membership model: primary always present,
// mirrors listed separately, replace-set semantics, per-destination retention.
func TestCameraDestinations(t *testing.T) {
	s := testStore(t)
	seedCam(t, s, "c1", "Cam", "")

	// Seeded: sole membership = the default location, and it is the primary.
	all, err := s.DestinationsForCamera("c1")
	if err != nil || len(all) != 1 || all[0].LocationID != "" {
		t.Fatalf("seed membership wrong: %+v err=%v", all, err)
	}
	if m, _ := s.MirrorDestinationsForCamera("c1"); len(m) != 0 {
		t.Fatalf("no mirrors expected, got %+v", m)
	}

	// Add a NAS mirror with its own retention.
	nas := &StorageLocation{Name: "NAS", Type: LocationSMB, AllocationBytes: 1 << 40, Enabled: true}
	mustNoErr(t, s.CreateStorageLocation(nas))
	mustNoErr(t, s.SetCameraDestinations("c1", "", []CameraDestination{
		{CameraID: "c1", LocationID: ""},
		{CameraID: "c1", LocationID: nas.ID, RetentionDays: 14},
	}))
	m, _ := s.MirrorDestinationsForCamera("c1")
	if len(m) != 1 || m[0].LocationID != nas.ID || m[0].RetentionDays != 14 {
		t.Fatalf("mirror set wrong: %+v", m)
	}

	// Swap the primary to the NAS: cameras.location_id follows.
	mustNoErr(t, s.SetCameraDestinations("c1", nas.ID, []CameraDestination{
		{CameraID: "c1", LocationID: ""},
		{CameraID: "c1", LocationID: nas.ID, RetentionDays: 14},
	}))
	cam, _ := s.GetCamera("c1")
	if cam.LocationID != nas.ID {
		t.Fatalf("primary swap did not update cameras.location_id: %q", cam.LocationID)
	}
	if m, _ := s.MirrorDestinationsForCamera("c1"); len(m) != 1 || m[0].LocationID != "" {
		t.Fatalf("after swap the default should be the mirror: %+v", m)
	}
}

// TestSegmentCopiesAndPromote pins the copy lifecycle: needing-copy detection,
// physical accounting, and promotion when the canonical copy dies.
func TestSegmentCopiesAndPromote(t *testing.T) {
	s := testStore(t)
	seedCam(t, s, "c1", "Cam", "")
	nas := &StorageLocation{Name: "NAS", Type: LocationSMB, Enabled: true}
	mustNoErr(t, s.CreateStorageLocation(nas))

	seg := seedSeg(t, s, "c1", "", 1_000_000, 100)

	// The segment needs a copy on the NAS; after inserting one it no longer does.
	need, _ := s.SegmentsNeedingCopy("c1", nas.ID, 0, 10)
	if len(need) != 1 || need[0].ID != seg.ID {
		t.Fatalf("needing-copy wrong: %+v", need)
	}
	mustNoErr(t, s.InsertSegmentCopy(&SegmentCopy{SegmentID: seg.ID, LocationID: nas.ID, Path: "c1/1000.ts", SizeBytes: 100}))
	if need, _ := s.SegmentsNeedingCopy("c1", nas.ID, 0, 10); len(need) != 0 {
		t.Fatalf("copy exists, nothing should need copying: %+v", need)
	}

	// Physical accounting: the NAS now physically holds 100 bytes of mirrors.
	if b, _ := s.TotalCopyBytesInLocation(nas.ID); b != 100 {
		t.Fatalf("copy bytes = %d, want 100", b)
	}
	if b, _ := s.TotalBytesInLocation(nas.ID); b != 100 {
		t.Fatalf("location physical bytes = %d, want 100 (copies count)", b)
	}

	// Promote: the copy becomes the canonical row; playback follows transparently.
	mustNoErr(t, s.PromoteCopy(seg.ID, nas.ID))
	got, _ := s.SegmentByID(seg.ID)
	if got.LocationID != nas.ID || got.Path != "c1/1000.ts" {
		t.Fatalf("promotion did not rewrite the row: %+v", got)
	}
	if copies, _ := s.CopiesForSegment(seg.ID); len(copies) != 0 {
		t.Fatalf("copy row should be consumed by promotion: %+v", copies)
	}
}

// TestOldestEvictablesMergesKinds pins that per-location cap eviction sees BOTH
// canonical segments and mirror copies, oldest first across kinds.
func TestOldestEvictablesMergesKinds(t *testing.T) {
	s := testStore(t)
	seedCam(t, s, "c1", "Cam", "")
	nas := &StorageLocation{Name: "NAS", Type: LocationSMB, Enabled: true}
	mustNoErr(t, s.CreateStorageLocation(nas))

	// On the NAS: a canonical segment at t=2000 and a mirror copy of a t=1000
	// segment whose canonical lives on the default location.
	older := seedSeg(t, s, "c1", "", 1_000, 10)
	mustNoErr(t, s.InsertSegmentCopy(&SegmentCopy{SegmentID: older.ID, LocationID: nas.ID, Path: "c1/1.ts", SizeBytes: 10}))
	newer := seedSeg(t, s, "c1", nas.ID, 2_000, 10)

	evs, err := s.OldestEvictablesInLocation(nas.ID, 10)
	if err != nil || len(evs) != 2 {
		t.Fatalf("want 2 evictables, got %d err=%v", len(evs), err)
	}
	if !evs[0].IsCopy || evs[0].Seg.ID != older.ID {
		t.Fatalf("oldest unit should be the t=1000 COPY, got %+v", evs[0])
	}
	if evs[1].IsCopy || evs[1].Seg.ID != newer.ID {
		t.Fatalf("second unit should be the t=2000 canonical, got %+v", evs[1])
	}
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
