package export

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestEmitExportStateFiresOnTerminal covers the change-feed gap that let a
// finished or failed export sit at "running" in the UI until the view was
// remounted: FinishExport/FailExport write the DB directly and emit nothing, so
// the manager must fire export.progress itself on the terminal transition. The
// message is the client's signal to refetch the list. Verifies both terminal
// paths emit, and that success carries the stored 100%.
func TestEmitExportStateFiresOnTerminal(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()
	m := New(st, nil, t.TempDir(), slog.Default())

	var got []float64
	m.SetOnProgress(func(_ string, p float64) { got = append(got, p) })

	// Success path: FinishExport stores progress=1, so the terminal emit carries it.
	done := &store.ExportJob{ID: "e1", CameraID: "c1", Mode: "single", Status: "running"}
	mustExport(t, st.CreateExport(done))
	mustExport(t, st.FinishExport(done.ID, "/tmp/x.mp4", 10, 0))
	m.emitExportState(done.ID)
	if len(got) != 1 || got[0] != 1.0 {
		t.Fatalf("after FinishExport: emitted %v, want [1]", got)
	}

	// Failure path: still emits so the list leaves "running" (the value is the
	// stored progress; the client refetches and reads status=error regardless).
	failed := &store.ExportJob{ID: "e2", CameraID: "c1", Mode: "single", Status: "running"}
	mustExport(t, st.CreateExport(failed))
	mustExport(t, st.FailExport(failed.ID, "boom", 0))
	m.emitExportState(failed.ID)
	if len(got) != 2 {
		t.Fatalf("after FailExport: got %d emissions, want 2", len(got))
	}
}

func mustExport(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
