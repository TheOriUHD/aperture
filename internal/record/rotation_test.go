package record

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/storage"
	"github.com/TheOriUHD/aperture/internal/store"
)

func newRecorder(t *testing.T, cfg *config.Config) (*Recorder, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(cfg, st, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil))), st
}

// A jump inside a segment probed as 18 hours on a real camera, ending in the future.
func TestWrittenDuration(t *testing.T) {
	const epoch = 1_000_000
	written := func(s float64) time.Time {
		return time.Unix(epoch, 0).Add(time.Duration(s * float64(time.Second)))
	}
	for _, c := range []struct {
		name          string
		probed, after float64
		want          float64
	}{
		{"normal segment", 10, 10, 10},
		{"probe slightly over", 12, 10, 12},
		{"timestamp jump", 64957, 2, 2},
		{"frozen timestamps stay as probed", 6.7, 7000, 6.7},
		{"unreadable stays unreadable", 0, 10, 0},
		{"mtime before the name is no evidence", 10, -5, 10},
	} {
		if got := writtenDuration(c.probed, epoch, written(c.after)); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// Without this a reconnect moved a camera's timestamps by hours: 2-second segments
// on one camera, a single 1.3 GB file on the other.
func TestRecordingTimestampsComeFromTheClock(t *testing.T) {
	for _, reencode := range []bool{false, true} {
		args := inputArgs("rtsp://restream/cam", reencode)
		flag := slices.Index(args, "-use_wallclock_as_timestamps")
		if flag < 0 || args[flag+1] != "1" {
			t.Fatalf("recording trusts the stream's timestamps: %v", args)
		}
		// An ffmpeg input option only applies to the -i after it.
		if input := slices.Index(args, "-i"); flag > input {
			t.Errorf("the flag comes after -i, where it does nothing: %v", args)
		}
	}
}

// Uncapped, a re-encoder's decoder and encoder each size their thread pools to the
// host and held 1.3 GB for one 4K camera. Stream copy decodes nothing, so it takes
// no cap.
func TestReencodeCapsThreads(t *testing.T) {
	in := inputArgs("rtsp://restream/cam", true)
	th, input := slices.Index(in, "-threads"), slices.Index(in, "-i")
	if th < 0 || th > input || in[th+1] != reencodeThreads {
		t.Errorf("decoder threads not capped before -i: %v", in)
	}
	if slices.Contains(inputArgs("rtsp://restream/cam", false), "-threads") {
		t.Error("stream copy should not set decoder threads")
	}
	out := videoArgs(store.Camera{RecordHeight: 1080}, true)
	if th := slices.Index(out, "-threads"); th < 0 || out[th+1] != reencodeThreads {
		t.Errorf("encoder threads not capped: %v", out)
	}
}

func TestNotRotating(t *testing.T) {
	now := time.Now()
	if notRotating(maxSegmentBytes, now, now) {
		t.Error("a file at the cap is still allowed")
	}
	if !notRotating(maxSegmentBytes+1, now, now) {
		t.Error("a file over the cap and still growing is stuck")
	}
	if notRotating(maxSegmentBytes+1, now.Add(-time.Minute), now) {
		t.Error("a file nobody is writing is finished, not stuck")
	}
}

// The net used to live only in the local indexer, so a camera on a network share
// could write one file for hours. The file is sparse: this allocates nothing.
func TestUploaderRestartsACameraThatStoppedRotating(t *testing.T) {
	for _, c := range []struct {
		name    string
		size    int64
		restart bool
	}{
		{"normal segment", 1 << 20, false},
		{"over the cap", maxSegmentBytes + 1, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := &config.Config{DataDir: t.TempDir()}
			r, st := newRecorder(t, cfg)
			r.reg = storage.NewRegistry(func(string) (*storage.Spec, error) {
				return nil, errors.New("share is down")
			})
			loc := &store.StorageLocation{Name: "NAS", Type: store.LocationSMB,
				Config: map[string]any{}, Enabled: true}
			if err := st.CreateStorageLocation(loc); err != nil {
				t.Fatal(err)
			}
			cam := &store.Camera{ID: "c1", Name: "c1", RetentionDays: 7, LocationID: loc.ID}
			if err := st.CreateCamera(cam); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(cfg.SpoolDir(), cam.ID)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%d.ts", time.Now().Unix())))
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Truncate(c.size); err != nil {
				t.Fatal(err)
			}
			f.Close()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r.active[cam.ID] = &camProc{stop: cancel, done: make(chan struct{})}
			r.uploadOnce()
			if restarted := ctx.Err() != nil; restarted != c.restart {
				t.Errorf("restarted = %v, want %v", restarted, c.restart)
			}
		})
	}
}

func TestNeedsRecovery(t *testing.T) {
	cfg := &config.Config{RecordingsDir: t.TempDir()}
	r, _ := newRecorder(t, cfg)
	cam := &store.Camera{ID: "c1", Name: "c1", RTSPMainURL: "rtsp://cam/1", Enabled: true,
		RecordMode: store.ModeContinuous}

	if !r.NeedsRecovery(cam) {
		t.Error("no footage directory at all: nothing is being written")
	}
	dir := filepath.Join(cfg.RecordingsDir, cam.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "100.ts")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r.NeedsRecovery(cam) {
		t.Error("ffmpeg is writing: this footage is only waiting on an upload")
	}
	quiet := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(file, quiet, quiet); err != nil {
		t.Fatal(err)
	}
	if !r.NeedsRecovery(cam) {
		t.Error("the newest file went quiet: the recorder has stalled")
	}

	scheduled := *cam
	scheduled.RecordMode = store.ModeScheduled
	if r.NeedsRecovery(&scheduled) {
		t.Error("outside its schedule a camera isn't meant to be recording")
	}
	off := *cam
	off.Enabled = false
	if r.NeedsRecovery(&off) {
		t.Error("a disabled camera isn't meant to be recording")
	}

}

// Through the real ffprobe: the row is what playback and the watchdog read.
func TestIndexerDoesNotBelieveAnImpossibleDuration(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	cfg := &config.Config{RecordingsDir: t.TempDir()}
	r, st := newRecorder(t, cfg)
	if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "c1", RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cfg.RecordingsDir, "c1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	epoch := time.Now().Add(-10 * time.Minute).Unix()
	file := filepath.Join(dir, fmt.Sprintf("%d.ts", epoch))
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi",
		"-i", "testsrc2=size=64x64:rate=5:duration=8", "-c:v", "libx264", "-preset", "ultrafast",
		"-f", "mpegts", file).CombinedOutput(); err != nil {
		t.Fatalf("make segment: %v: %s", err, out)
	}
	wrote := time.Unix(epoch+1, 0)
	if err := os.Chtimes(file, wrote, wrote); err != nil {
		t.Fatal(err)
	}

	r.indexOnce()

	stat, err := st.CameraStat("c1")
	if err != nil {
		t.Fatal(err)
	}
	if want := (epoch + 1) * 1000; stat.NewestTS != want {
		t.Errorf("segment ends at %d, want %d (the second its file was last written)", stat.NewestTS, want)
	}
}
