package store

import "testing"

// TestReassignCameras covers T0.3: force-deleting a storage location moves every
// camera on it back to the default atomically (all-or-nothing), returning the
// moved ids so the caller can restart their recorders.
func TestReassignCameras(t *testing.T) {
	st := testStore(t)

	mkCam := func(id, loc string) {
		if err := st.CreateCamera(&Camera{ID: id, Name: id}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		if loc != DefaultLocationID {
			if err := st.SetCameraLocation(id, loc); err != nil {
				t.Fatalf("set loc %s: %v", id, err)
			}
		}
	}
	mkCam("a", "loc1")
	mkCam("b", "loc1")
	mkCam("c", DefaultLocationID)

	moved, err := st.ReassignCameras("loc1", DefaultLocationID)
	if err != nil {
		t.Fatalf("reassign: %v", err)
	}
	if len(moved) != 2 {
		t.Fatalf("expected 2 cameras moved, got %d (%v)", len(moved), moved)
	}
	if n, _ := st.CamerasUsingLocation("loc1"); n != 0 {
		t.Errorf("loc1 should have 0 cameras after reassign, got %d", n)
	}
	if n, _ := st.CamerasUsingLocation(DefaultLocationID); n != 3 {
		t.Errorf("default should hold all 3 cameras, got %d", n)
	}

	// No cameras left ⇒ no-op, no error, no ids.
	moved, err = st.ReassignCameras("loc1", DefaultLocationID)
	if err != nil || len(moved) != 0 {
		t.Errorf("empty reassign should be a clean no-op: ids=%v err=%v", moved, err)
	}
}
