package record

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Through the real ffmpeg: stopping a recording must leave the segment it was
// writing complete. With exec.CommandContext's default SIGKILL this exact
// command left a 0-byte, unreadable file — at a low bitrate everything was
// still in ffmpeg's buffers — and on a camera it cut the tail of every segment
// that was open at a restart.
func TestRecordCmdClosesSegmentOnStop(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin",
		"-re", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000",
		"-map", "0:v:0", "-map", "1:a",
		"-c:v", "mpeg2video", "-g", "25", "-c:a", "aac", "-b:a", "32k",
		"-f", "segment", "-segment_time", "10", "-segment_format", "mpegts",
		"-strftime", "1", filepath.Join(dir, "%s.ts"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := recordCmd(ctx, args)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second) // well inside the first 10 s segment
	cancel()
	stopped := time.Now()
	_ = cmd.Wait()
	if took := time.Since(stopped); took >= ffmpegStopGrace {
		t.Fatalf("ffmpeg took %s to stop: it was killed, not finished", took)
	}
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		t.Fatalf("ffmpeg died of %v instead of exiting after finishing its segment", ws.Signal())
	}

	files, _ := filepath.Glob(filepath.Join(dir, "*.ts"))
	if len(files) != 1 {
		t.Fatalf("want the one open segment, got %v", files)
	}
	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 || info.Size()%188 != 0 {
		t.Fatalf("segment is %d bytes: not closed on a TS packet boundary", info.Size())
	}
	if d := probeDuration(files[0]); d < 1 {
		t.Fatalf("segment probes as %.2fs, want the ~2s recorded before the stop", d)
	}
}

// fakeCams stands in for superviseFFmpeg: each "ffmpeg" runs until its context
// ends and then takes a while to finish its segment, like the real one does.
type fakeCams struct {
	finish time.Duration

	mu      sync.Mutex
	running map[string]int // camera -> supervisors recording right now
	overlap bool           // two supervisors ever recorded one camera at once
	started map[string]int
}

func (f *fakeCams) run(ctx context.Context, cam store.Camera) {
	f.mu.Lock()
	f.running[cam.ID]++
	f.started[cam.ID]++
	if f.running[cam.ID] > 1 {
		f.overlap = true
	}
	f.mu.Unlock()
	<-ctx.Done()
	time.Sleep(f.finish) // flushing and closing the segment
	f.mu.Lock()
	f.running[cam.ID]--
	f.mu.Unlock()
}

func (f *fakeCams) snapshot() (running, started map[string]int, overlap bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	running, started = map[string]int{}, map[string]int{}
	for k, v := range f.running {
		running[k] = v
	}
	for k, v := range f.started {
		started[k] = v
	}
	return running, started, f.overlap
}

// Run must not return on shutdown until every camera's ffmpeg has finished, and a
// restarted camera must not start recording until its previous ffmpeg is gone.
func TestRunWaitsForCamerasAndNeverOverlaps(t *testing.T) {
	cfg := &config.Config{RecordingsDir: t.TempDir()}
	r, st := newRecorder(t, cfg)
	for _, id := range []string{"c1", "c2"} {
		mustCreate(t, st, &store.Camera{ID: id, Name: id, RTSPMainURL: "rtsp://cam/" + id,
			Enabled: true, RecordMode: store.ModeContinuous})
	}
	fake := &fakeCams{finish: 300 * time.Millisecond, running: map[string]int{}, started: map[string]int{}}
	r.runCam = fake.run

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ran := make(chan struct{})
	go func() { r.Run(ctx); close(ran) }()

	waitFor(t, func() bool { running, _, _ := fake.snapshot(); return running["c1"] == 1 && running["c2"] == 1 })

	// A restart must wait for the old process to finish its segment first.
	r.RestartCamera("c1")
	r.RestartCamera("c1") // twice in a row: the chain still holds
	waitFor(t, func() bool { running, started, _ := fake.snapshot(); return started["c1"] == 3 && running["c1"] == 1 })
	if _, _, overlap := fake.snapshot(); overlap {
		t.Fatal("two recorders ran for one camera at once")
	}

	cancel()
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
	if running, _, _ := fake.snapshot(); running["c1"] != 0 || running["c2"] != 0 {
		t.Fatalf("Run returned with recorders still finishing: %v", running)
	}

	// After shutdown nothing may start again, from any caller.
	r.Reconcile()
	r.RestartCamera("c2")
	time.Sleep(50 * time.Millisecond)
	if running, _, _ := fake.snapshot(); running["c2"] != 0 {
		t.Fatal("a recorder started after shutdown")
	}
}

func mustCreate(t *testing.T, st *store.Store, c *store.Camera) {
	t.Helper()
	if err := st.CreateCamera(c); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
