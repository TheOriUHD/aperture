package store

import "testing"

// TestPreviewEvictionInLocation pins the arithmetic behind the "this will delete
// footage" confirmation. It must mirror the janitor's oldest-first eviction exactly,
// or the dialog would promise one thing and the janitor do another.
func TestPreviewEvictionInLocation(t *testing.T) {
	s := testStore(t)
	for _, c := range []*Camera{{ID: "cam1", Name: "Front"}, {ID: "cam2", Name: "Back"}} {
		if err := s.CreateCamera(c); err != nil {
			t.Fatalf("create camera: %v", err)
		}
	}

	// 10 segments of 100 bytes each, oldest first, alternating cameras.
	for i := 0; i < 10; i++ {
		cam := "cam1"
		if i%2 == 1 {
			cam = "cam2"
		}
		start := int64(1000 + i*10)
		if err := s.InsertSegment(&Segment{
			ID: NewID(), CameraID: cam, Path: "/x/" + NewID() + ".ts",
			StartTS: start, EndTS: start + 10, DurationMS: 10000, SizeBytes: 100,
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Target above usage → nothing evicted (an increase must never destroy).
	p, err := s.PreviewEvictionInLocation("", 5000)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if p.Bytes != 0 || p.Segments != 0 {
		t.Errorf("increase should evict nothing, got %d bytes / %d segments", p.Bytes, p.Segments)
	}

	// Usage is 1000; target 700 must free 300 → the 3 OLDEST segments.
	p, err = s.PreviewEvictionInLocation("", 700)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if p.Segments != 3 || p.Bytes != 300 {
		t.Errorf("target 700: evicts %d segments / %d bytes, want 3 / 300", p.Segments, p.Bytes)
	}
	if p.OldestTS != 1000 {
		t.Errorf("must start at the OLDEST segment, got ts %d want 1000", p.OldestTS)
	}
	// Those 3 are cam1,cam2,cam1 → the per-camera split the dialog shows.
	if got := p.PerCamera["cam1"]; got == nil || got.Segments != 2 {
		t.Errorf("cam1 share = %+v, want 2 segments", got)
	}
	if got := p.PerCamera["cam2"]; got == nil || got.Segments != 1 {
		t.Errorf("cam2 share = %+v, want 1 segment", got)
	}

	// Target 0 → everything goes.
	p, err = s.PreviewEvictionInLocation("", 0)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if p.Segments != 10 || p.Bytes != 1000 {
		t.Errorf("target 0: evicts %d/%d, want 10/1000", p.Segments, p.Bytes)
	}
}
