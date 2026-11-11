package watchdog

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
)

type calls struct {
	mu  sync.Mutex
	log []string
}

func (c *calls) add(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.log = append(c.log, s)
}

type fakeRecorder struct {
	due     bool
	stalled bool
	calls   *calls
}

func (f *fakeRecorder) RecordingDue(*store.Camera) bool  { return f.due }
func (f *fakeRecorder) NeedsRecovery(*store.Camera) bool { return f.stalled }
func (f *fakeRecorder) RestartCamera(string)             { f.calls.add("recorder") }

// staleCamera is a camera whose newest indexed footage is five minutes old, watched
// against a go2rtc that notes every stream registration.
func staleCamera(t *testing.T, rec Recorder, c *calls) (*Watchdog, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "c1", RTSPMainURL: "rtsp://cam/1",
		Enabled: true, RecordMode: store.ModeContinuous, RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-5 * time.Minute).UnixMilli()
	if err := st.InsertSegment(&store.Segment{CameraID: "c1", Path: "c1/1.ts", StartTS: start,
		EndTS: start + 10_000, DurationMS: 10_000, SizeBytes: 1}); err != nil {
		t.Fatal(err)
	}
	go2rtc := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			c.add("stream")
		}
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(rw, "{}")
		}
	}))
	t.Cleanup(go2rtc.Close)
	sm, err := stream.NewManager(go2rtc.URL, "rtsp://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	return New(st, sm, rec, slog.New(slog.NewTextHandler(io.Discard, nil))), st
}

// A NAS outage leaves the index stale while ffmpeg records happily. Restarting the
// stream under it jumped its timestamps, which took recording down with the NAS.
func TestFailingUploadLeavesTheStreamAlone(t *testing.T) {
	c := &calls{}
	w, _ := staleCamera(t, &fakeRecorder{due: true, stalled: false, calls: c}, c)

	w.tick(context.Background())

	if len(c.log) != 0 {
		t.Errorf("a healthy recorder was restarted: %v", c.log)
	}
	if _, _, stale := w.Stats("c1"); !stale {
		t.Error("footage isn't arriving, so health must still say so")
	}
}

func TestStalledRecorderRestartsStreamThenFFmpeg(t *testing.T) {
	c := &calls{}
	w, _ := staleCamera(t, &fakeRecorder{due: true, stalled: true, calls: c}, c)

	w.tick(context.Background())

	if want := []string{"stream", "recorder"}; !slices.Equal(c.log, want) {
		t.Errorf("recovery did %v, want %v", c.log, want)
	}
}

// A camera recording 07:00-23:00 has hours-old footage all night, on purpose. That
// must not read as stale: a dashboard raises an alert for it, and the change feed would
// call a working camera offline every night.
func TestOutsideItsScheduleACameraIsNotStale(t *testing.T) {
	c := &calls{}
	w, _ := staleCamera(t, &fakeRecorder{due: false, stalled: true, calls: c}, c)
	states := make(chan bool, 1)
	w.SetOnCameraState(func(_ string, online bool) { states <- online })
	w.probe = func(context.Context, string) error { return nil } // nothing reads it; it answers

	w.tick(context.Background())

	if _, _, stale := w.Stats("c1"); stale {
		t.Error("reported stale outside its schedule")
	}
	if len(c.log) != 0 {
		t.Errorf("tried to recover a camera that is idle by design: %v", c.log)
	}
	select {
	case online := <-states:
		if !online {
			t.Error("reported offline outside its schedule")
		}
	case <-time.After(time.Second):
		t.Fatal("no camera state reported")
	}
}
