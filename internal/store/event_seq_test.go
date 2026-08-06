package store

import (
	"path/filepath"
	"testing"
)

// TestEventSeqStrictlyIncreasing covers T1.5: each inserted event gets a strictly
// increasing seq (monotonic, not asserted gapless).
func TestEventSeqStrictlyIncreasing(t *testing.T) {
	st := testStore(t)
	if err := st.CreateCamera(&Camera{ID: "cam1", Name: "1"}); err != nil {
		t.Fatal(err)
	}
	var prev int64
	for i := 0; i < 5; i++ {
		e := &Event{CameraID: "cam1", Type: "motion", StartTS: int64(1000 + i)}
		if err := st.InsertEvent(e); err != nil {
			t.Fatal(err)
		}
		if e.Seq <= prev {
			t.Fatalf("seq must be strictly increasing: got %d after %d", e.Seq, prev)
		}
		prev = e.Seq
	}
}

// TestEventSeqPersistsAcrossReopen covers the boot-time re-seed from MAX(seq):
// a reopened store continues the sequence instead of colliding.
func TestEventSeqPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateCamera(&Camera{ID: "cam1", Name: "1"}); err != nil {
		t.Fatal(err)
	}
	var last int64
	for i := 0; i < 3; i++ {
		e := &Event{CameraID: "cam1", StartTS: int64(i)}
		if err := st.InsertEvent(e); err != nil {
			t.Fatal(err)
		}
		last = e.Seq
	}
	_ = st.Close()

	st2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	e := &Event{CameraID: "cam1", StartTS: 99}
	if err := st2.InsertEvent(e); err != nil {
		t.Fatal(err)
	}
	if e.Seq != last+1 {
		t.Errorf("seq should continue at %d after reopen, got %d", last+1, e.Seq)
	}
}
