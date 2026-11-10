package store

import "testing"

func TestTrackPointsRoundTripAndCascade(t *testing.T) {
	s := testStore(t)
	if err := s.CreateCamera(&Camera{ID: "cam", Name: "Cam"}); err != nil {
		t.Fatal(err)
	}
	e := &Event{ID: "ev1", CameraID: "cam", Type: "motion", StartTS: 1000}
	if err := s.InsertEvent(e); err != nil {
		t.Fatal(err)
	}
	pts := []TrackPoint{
		{EventID: "ev1", Obj: 7, Label: "person", Confidence: 0.9, X: 0.1, Y: 0.2, W: 0.1, H: 0.5, TS: 1100},
		{EventID: "ev1", Obj: 7, Label: "person", Confidence: 0.92, X: 0.15, Y: 0.2, W: 0.1, H: 0.5, TS: 1400},
	}
	if err := s.AddTrackPoints("cam", pts); err != nil {
		t.Fatal(err)
	}
	got, err := s.TrackPoints("cam", 1000, 2000, 100)
	if err != nil || len(got) != 2 || got[1].X != 0.15 || got[0].Obj != 7 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if other, _ := s.TrackPoints("other", 0, 5000, 100); len(other) != 0 {
		t.Fatal("another camera's points leaked")
	}
	if err := s.DeleteEvent("ev1"); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.TrackPoints("cam", 0, 5000, 100); len(left) != 0 {
		t.Fatalf("points outlived their event: %d", len(left))
	}
}
