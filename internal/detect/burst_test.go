package detect

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestLoadConfigClampsBurst covers T0.4: the detection engine clamps Burst at
// read time, so no settings value — however it was written (API, config
// restore, or a direct DB edit) — can drive the per-event sampling loop past
// MaxBurst.
func TestLoadConfigClampsBurst(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	e := New(st, nil, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	// An out-of-range burst (e.g. a hostile backup restore) must clamp down.
	if err := st.SetSetting("detection", `{"mode":"bridge","burst":9999}`); err != nil {
		t.Fatal(err)
	}
	if got := e.LoadConfig().Burst; got != MaxBurst {
		t.Errorf("burst not clamped: got %d, want %d", got, MaxBurst)
	}

	// The existing lower clamp still holds.
	if err := st.SetSetting("detection", `{"mode":"bridge","burst":0}`); err != nil {
		t.Fatal(err)
	}
	if got := e.LoadConfig().Burst; got != 1 {
		t.Errorf("burst lower clamp: got %d, want 1", got)
	}
}
