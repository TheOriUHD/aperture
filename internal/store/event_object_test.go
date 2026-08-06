package store

import "testing"

// TestEventObjectIDsAddressable covers T1.6: each saved detection gets a distinct
// non-zero id (so /events/{id}/detections/{detId} can address one), read back by
// both the single and batch loaders.
func TestEventObjectIDsAddressable(t *testing.T) {
	st := testStore(t)
	if err := st.CreateCamera(&Camera{ID: "cam1", Name: "1"}); err != nil {
		t.Fatal(err)
	}
	e := &Event{CameraID: "cam1", StartTS: 1}
	if err := st.InsertEvent(e); err != nil {
		t.Fatal(err)
	}
	objs := []EventObject{{Label: "person", Confidence: 0.9}, {Label: "car", Confidence: 0.8}}
	if err := st.SaveEventDetections(e.ID, "person", "person, car", objs); err != nil {
		t.Fatal(err)
	}

	got, err := st.EventObjects(e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 objects, got %d", len(got))
	}
	seen := map[int64]bool{}
	for _, o := range got {
		if o.ID == 0 {
			t.Errorf("object %q has no id", o.Label)
		}
		if seen[o.ID] {
			t.Errorf("duplicate object id %d", o.ID)
		}
		seen[o.ID] = true
	}

	// Batch loader returns the same ids.
	batch, err := st.EventObjectsFor([]string{e.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch[e.ID]) != 2 || batch[e.ID][0].ID == 0 {
		t.Errorf("batch loader lost ids: %+v", batch[e.ID])
	}
}
