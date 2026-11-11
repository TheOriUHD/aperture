package watchdog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
)

// rig is a watchdog on one camera, "c1", against a go2rtc whose answer for the
// camera's stream the test sets, with a clock and a probe the test controls.
type rig struct {
	w     *Watchdog
	clock time.Time

	mu        sync.Mutex
	connected bool  // go2rtc has a live connection to the camera
	recv      int64 // what it has received over it
	consumers int
	probeErr  error
	probes    int
	states    []bool
}

func newRig(t *testing.T, due bool) *rig {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "Door", RTSPMainURL: "rtsp://cam/1",
		Enabled: true, RecordMode: store.ModeContinuous, RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	r := &rig{clock: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)}
	go2rtc := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		producer := `{"url":"rtsp://cam/1"}`
		if r.connected {
			producer = fmt.Sprintf(`{"id":1,"remote_addr":"10.0.0.9:554","bytes_recv":%d}`, r.recv)
		}
		consumers := "[]"
		if r.consumers > 0 {
			consumers = `[{"id":2}]`
		}
		_, _ = fmt.Fprintf(rw, `{%q:{"producers":[%s],"consumers":%s}}`, stream.StreamName("c1"), producer, consumers)
	}))
	t.Cleanup(go2rtc.Close)
	sm, err := stream.NewManager(go2rtc.URL, "rtsp://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	r.w = New(st, sm, &fakeRecorder{due: due, calls: &calls{}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.w.now = func() time.Time { return r.clock }
	r.w.probe = func(context.Context, string) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.probes++
		return r.probeErr
	}
	r.w.SetOnCameraState(func(_ string, online bool) {
		r.mu.Lock()
		r.states = append(r.states, online)
		r.mu.Unlock()
	})
	return r
}

// tick advances the clock by d and runs one check.
func (r *rig) tick(d time.Duration) {
	r.clock = r.clock.Add(d)
	r.w.tick(context.Background())
}

func (r *rig) set(f func(r *rig)) {
	r.mu.Lock()
	f(r)
	r.mu.Unlock()
}

func (r *rig) expect(t *testing.T, step string, online bool) {
	t.Helper()
	if got := r.w.Online("c1"); got != online {
		t.Fatalf("%s: online %v, want %v", step, got, online)
	}
}

// lastState waits for the camera.state reports to settle (they're sent async)
// and returns the last one.
func (r *rig) lastState(t *testing.T) (bool, int) {
	t.Helper()
	time.Sleep(20 * time.Millisecond)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.states) == 0 {
		t.Fatal("no camera state reported")
	}
	return r.states[len(r.states)-1], len(r.states)
}

// A recording camera that stops sending: go2rtc still holds its stream, which
// is all "online" used to check, but nothing arrives.
func TestCameraThatStopsSendingGoesOffline(t *testing.T) {
	r := newRig(t, true)
	r.set(func(r *rig) { r.connected, r.recv = true, 1000 })
	r.tick(0)
	r.expect(t, "sending", true)
	r.set(func(r *rig) { r.recv = 2000 })
	r.tick(interval)
	r.expect(t, "still sending", true)

	r.set(func(r *rig) { r.connected, r.recv = false, 0 }) // unplugged
	r.tick(interval)
	r.expect(t, "15 s without video", true)
	r.tick(interval)
	r.expect(t, "30 s without video", false)
	if on, _ := r.lastState(t); on {
		t.Fatal("the change feed wasn't told it went offline")
	}

	r.set(func(r *rig) { r.connected, r.recv = true, 500 }) // back
	r.tick(interval)
	r.expect(t, "sending again", true)
	if on, _ := r.lastState(t); !on {
		t.Fatal("the change feed wasn't told it's back")
	}
}

// A connection that stays open with nothing coming over it is just as offline.
func TestStalledStreamGoesOffline(t *testing.T) {
	r := newRig(t, true)
	r.set(func(r *rig) { r.connected, r.recv = true, 1000 })
	r.tick(0)
	r.tick(interval)
	r.tick(interval)
	r.expect(t, "30 s of the same byte count", false)
}

// Starting up, a camera being read gets offlineAfter to deliver before it
// counts as offline.
func TestStartingCameraGetsTimeToDeliver(t *testing.T) {
	r := newRig(t, true)
	r.tick(0)
	r.expect(t, "just started", true)
	r.tick(interval)
	r.expect(t, "15 s in", true)
	r.tick(interval)
	r.expect(t, "30 s in and nothing came", false)
}

// A camera nothing reads (no recording, no recognition, no viewer) is asked
// for: once a minute when it answers, offline after two failed asks in a row.
func TestCameraNothingReadsIsAsked(t *testing.T) {
	r := newRig(t, false)
	r.tick(0)
	r.expect(t, "answers", true)
	r.tick(interval)
	r.tick(interval)
	r.mu.Lock()
	probes := r.probes
	r.mu.Unlock()
	if probes != 1 {
		t.Fatalf("asked %d times in 30 s, want once a minute", probes)
	}

	r.set(func(r *rig) { r.probeErr = errors.New("404") })
	r.tick(interval)
	r.tick(interval) // a minute after the last ask: it fails
	r.expect(t, "one failed ask", true)
	r.tick(interval) // asked again straight away
	r.expect(t, "two failed asks", false)

	r.set(func(r *rig) { r.probeErr = nil })
	r.tick(probeEvery)
	r.expect(t, "answers again", true)
}

// Opening live view on such a camera: go2rtc needs a moment to connect, which
// must not read as offline.
func TestViewerOpeningIsNotOffline(t *testing.T) {
	r := newRig(t, false)
	r.tick(0)
	r.expect(t, "answers", true)
	r.tick(10 * time.Minute)
	r.set(func(r *rig) { r.consumers = 1 }) // a viewer, still connecting
	r.tick(interval)
	r.expect(t, "viewer connecting", true)
	r.set(func(r *rig) { r.connected, r.recv = true, 4000 })
	r.tick(interval)
	r.expect(t, "viewer watching", true)
}
