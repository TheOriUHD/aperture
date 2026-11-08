package record

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestUntouchedCameraKeepsStreamCopy pins the promise that makes this feature safe
// to ship: a camera with no quality set runs the recording path it always did.
func TestUntouchedCameraKeepsStreamCopy(t *testing.T) {
	c := store.Camera{}
	if wantsReencode(c) {
		t.Fatal("a camera with no quality set must not re-encode")
	}
	if got := videoArgs(c, wantsReencode(c)); !slices.Equal(got, []string{"-c:v", "copy"}) {
		t.Fatalf("video args = %v, want plain stream copy", got)
	}
}

func TestVideoArgs(t *testing.T) {
	for _, c := range []struct {
		name          string
		cam           store.Camera
		want, wantNot []string
	}{
		{"resolution only", store.Camera{RecordHeight: 1080}, []string{"scale=-2:'min(1080,ih)'", "-crf"}, []string{"select=", "-b:v"}},
		{"framerate only", store.Camera{RecordFPS: 30}, []string{"select=", "-crf"}, []string{"scale=", "-b:v"}},
		{"bitrate only", store.Camera{RecordKbps: 2000}, []string{"-b:v 2000k", "-maxrate 2000k", "-bufsize 4000k"}, []string{"-vf", "-crf"}},
		{"everything", store.Camera{RecordHeight: 720, RecordFPS: 15, RecordKbps: 800}, []string{"select=", "scale=-2:'min(720,ih)'", "-b:v 800k"}, []string{"-crf"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if !wantsReencode(c.cam) {
				t.Fatal("should re-encode")
			}
			got := strings.Join(videoArgs(c.cam, true), " ")
			for _, w := range append(c.want, "-c:v libx264", "-force_key_frames", "-fps_mode vfr", "-pix_fmt yuv420p") {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in: %s", w, got)
				}
			}
			for _, w := range c.wantNot {
				if strings.Contains(got, w) {
					t.Errorf("unexpected %q in: %s", w, got)
				}
			}
			if strings.Contains(got, "fps=") {
				t.Errorf("the fps filter duplicates frames to reach its rate: %s", got)
			}
		})
	}
}

// TestFallsBackToTheOriginalStream pins that a failing re-encoder cannot lose
// footage: repeated quick exits stop the re-encoding, and a long run forgives.
func TestFallsBackToTheOriginalStream(t *testing.T) {
	quick := 0
	if !keepReencoding(time.Second, &quick) || !keepReencoding(time.Second, &quick) {
		t.Fatal("two quick exits should still retry")
	}
	if !keepReencoding(time.Hour, &quick) || quick != 0 {
		t.Fatalf("a long run should clear the count, got %d", quick)
	}
	keepReencoding(time.Second, &quick)
	keepReencoding(time.Second, &quick)
	if keepReencoding(time.Second, &quick) {
		t.Fatal("three quick exits in a row should fall back")
	}
	// Once it falls back the supervisor records with reencode=false.
	if got := videoArgs(store.Camera{RecordHeight: 1080}, false); !slices.Equal(got, []string{"-c:v", "copy"}) {
		t.Fatalf("fallback args = %v, want stream copy", got)
	}
}

// probe reads width, height and frame count from a media file.
func probe(t *testing.T, file string) (w, h, frames int) {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-count_packets",
		"-show_entries", "stream=width,height,nb_read_packets", "-of", "csv=p=0:s=x", file).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	p := strings.Split(strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]), "x")
	if len(p) != 3 {
		t.Fatalf("unexpected ffprobe output %q", out)
	}
	w, _ = strconv.Atoi(p[0])
	h, _ = strconv.Atoi(p[1])
	frames, _ = strconv.Atoi(p[2])
	return w, h, frames
}

// TestReencodeArgsThroughRealFFmpeg runs the arguments the way the recorder does,
// without a shell, so the quoting inside the filters is exercised for real.
func TestReencodeArgsThroughRealFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	run := func(cam store.Camera) (w, h, frames int) {
		t.Helper()
		dir := t.TempDir()
		args := []string{"-hide_banner", "-loglevel", "error", "-nostdin",
			"-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=20:duration=4"}
		args = append(args, videoArgs(cam, true)...)
		args = append(args, "-f", "segment", "-segment_time", "60", "-segment_format", "mpegts",
			filepath.Join(dir, "%d.ts"))
		var stderr bytes.Buffer
		cmd := exec.Command("ffmpeg", args...)
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("ffmpeg: %v\n%s", err, stderr.String())
		}
		if _, err := os.Stat(filepath.Join(dir, "0.ts")); err != nil {
			t.Fatalf("no segment written: %v", err)
		}
		return probe(t, filepath.Join(dir, "0.ts"))
	}

	if w, h, _ := run(store.Camera{RecordHeight: 480}); w != 854 || h != 480 {
		t.Errorf("scaled to %dx%d, want 854x480", w, h)
	}
	if w, h, _ := run(store.Camera{RecordHeight: 2160}); w != 1280 || h != 720 {
		t.Errorf("a ceiling above the source upscaled it to %dx%d", w, h)
	}
	if _, _, n := run(store.Camera{RecordFPS: 30}); n != 80 {
		t.Errorf("a cap above the 20 fps source gave %d frames in 4 s, want the original 80", n)
	}
	if _, _, n := run(store.Camera{RecordFPS: 10}); n < 38 || n > 42 {
		t.Errorf("a 10 fps cap gave %d frames in 4 s, want about 40", n)
	}
}
