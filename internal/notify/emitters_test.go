package notify

import (
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

// TestEphemeralEmitters covers camera.state and detection.created: both are
// ephemeral (no SSE id) and camera.state is snapshot-cached.
func TestEphemeralEmitters(t *testing.T) {
	e := &Engine{}
	_, ch := e.SubscribeFeed(FeedFilter{}, nil)

	e.OnCameraState("cam1", false)
	e.OnDetections("ev9", "cam1")

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case f := <-ch:
			if f.Durable {
				t.Error("ephemeral emitter must not set the durable SSE id")
			}
			var m map[string]any
			_ = json.Unmarshal(f.Data, &m)
			seen[m["type"].(string)] = true
		default:
			t.Fatal("expected two frames")
		}
	}
	if !seen["camera.state"] || !seen["detection.created"] {
		t.Fatalf("emitted %v, want camera.state + detection.created", seen)
	}
	if snap := e.EphemeralSnapshot(FeedFilter{Types: map[string]bool{"camera.state": true}}, nil); len(snap) != 1 {
		t.Fatalf("camera.state should be snapshot-cached, got %+v", snap)
	}
}

// TestEventClosedDurable covers event.closed carrying the event's seq (durable),
// so a resuming client sees it via Last-Event-ID replay.
func TestEventClosedDurable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	e := New(&config.Config{DataDir: t.TempDir()}, st, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_ = st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"})
	ev := &store.Event{CameraID: "cam1", StartTS: 1000}
	_ = st.InsertEvent(ev)
	_ = st.FinalizeEvent(ev.ID, 5000, 0.9, nil)

	_, ch := e.SubscribeFeed(FeedFilter{}, nil)
	e.OnEventClose(ev.ID)
	select {
	case f := <-ch:
		if !f.Durable || f.ID != ev.Seq {
			t.Errorf("event.closed: durable=%v id=%d, want durable with seq %d", f.Durable, f.ID, ev.Seq)
		}
		var m map[string]any
		_ = json.Unmarshal(f.Data, &m)
		if m["type"] != "event.closed" {
			t.Errorf("type = %v, want event.closed", m["type"])
		}
	default:
		t.Fatal("expected an event.closed frame")
	}
}
