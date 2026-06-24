// Package export turns a camera + time range into a downloadable MP4. With no
// burn-in it is a near-instant stream-copy concat of the recorded MPEG-TS
// segments (honoring the no-transcode record path); burn-in is an explicit,
// bounded transcode. Jobs run one at a time through a worker so a large export
// never starves recording.
package export

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

const (
	exportTTL = 24 * time.Hour
	fontFile  = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
)

// Manager owns the export output directory and the single-worker job queue.
type Manager struct {
	st    *store.Store
	dir   string
	log   *slog.Logger
	queue chan string
}

func New(st *store.Store, dir string, log *slog.Logger) *Manager {
	_ = os.MkdirAll(dir, 0o755)
	return &Manager{st: st, dir: dir, log: log, queue: make(chan string, 64)}
}

// Dir is the export output directory (download handlers read from here).
func (m *Manager) Dir() string { return m.dir }

// Start recovers interrupted jobs, then runs the worker + cleanup loops.
func (m *Manager) Start(ctx context.Context) {
	if pend, err := m.st.PendingExports(); err == nil {
		for _, j := range pend {
			m.Enqueue(j.ID)
		}
	}
	go m.worker(ctx)
	go m.cleanupLoop(ctx)
}

// Enqueue schedules a job for processing.
func (m *Manager) Enqueue(id string) {
	select {
	case m.queue <- id:
	default:
		go func() { m.queue <- id }()
	}
}

func (m *Manager) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-m.queue:
			m.process(ctx, id)
		}
	}
}

func (m *Manager) process(ctx context.Context, id string) {
	job, err := m.st.GetExport(id)
	if err != nil {
		return
	}
	_ = m.st.SetExportProgress(id, 0)
	fail := func(msg string) {
		m.log.Error("export failed", "id", id, "err", msg)
		_ = m.st.FailExport(id, msg, time.Now().Unix())
	}

	segs, err := m.st.ListSegments(job.CameraID, job.FromTS, job.ToTS)
	if err != nil {
		fail("could not list footage")
		return
	}
	if len(segs) == 0 {
		fail("no footage in the selected range")
		return
	}

	listPath := filepath.Join(m.dir, id+".txt")
	var b strings.Builder
	for _, sg := range segs {
		b.WriteString("file '")
		b.WriteString(sg.Path)
		b.WriteString("'\n")
	}
	if err := os.WriteFile(listPath, []byte(b.String()), 0o600); err != nil {
		fail("internal error")
		return
	}
	defer os.Remove(listPath)

	offset := float64(job.FromTS-segs[0].StartTS) / 1000.0
	if offset < 0 {
		offset = 0
	}
	durMs := job.ToTS - job.FromTS
	dur := float64(durMs) / 1000.0
	out := filepath.Join(m.dir, id+".mp4")

	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if job.BurnIn {
		args = append(args, "-progress", "pipe:1")
	}
	args = append(args, "-f", "concat", "-safe", "0", "-i", listPath,
		"-ss", ftoa(offset), "-t", ftoa(dur))
	if job.BurnIn {
		args = append(args, "-vf", drawtextVF(job.CameraName, job.FromTS/1000),
			"-c:v", "libx264", "-preset", "veryfast", "-crf", "22")
		if job.Audio {
			args = append(args, "-c:a", "aac")
		} else {
			args = append(args, "-an")
		}
	} else {
		args = append(args, "-c", "copy")
		if !job.Audio {
			args = append(args, "-an")
		}
	}
	args = append(args, "-movflags", "+faststart", "-y", out)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if job.BurnIn && durMs > 0 {
		if stdout, err := cmd.StdoutPipe(); err == nil {
			go m.trackProgress(id, stdout, durMs)
		}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := firstLine(stderr.String())
		if msg == "" {
			msg = "ffmpeg failed"
		}
		fail(msg)
		return
	}
	fi, err := os.Stat(out)
	if err != nil {
		fail("export produced no file")
		return
	}
	_ = m.st.FinishExport(id, out, fi.Size(), time.Now().Unix())
	m.log.Info("export done", "id", id, "bytes", fi.Size())
}

func (m *Manager) trackProgress(id string, r io.Reader, durMs int64) {
	sc := bufio.NewScanner(r)
	last := -1.0
	for sc.Scan() {
		line := sc.Text()
		v, ok := strings.CutPrefix(line, "out_time_us=")
		if !ok {
			continue
		}
		us, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			continue
		}
		p := float64(us) / 1000.0 / float64(durMs)
		if p > 0.99 {
			p = 0.99
		}
		if p-last >= 0.02 {
			last = p
			_ = m.st.SetExportProgress(id, p)
		}
	}
}

func (m *Manager) cleanupLoop(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	m.cleanup()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.cleanup()
		}
	}
}

func (m *Manager) cleanup() {
	cutoff := time.Now().Add(-exportTTL).Unix()
	jobs, err := m.st.ExportsBefore(cutoff)
	if err != nil {
		return
	}
	for _, j := range jobs {
		if j.OutputPath != "" {
			_ = os.Remove(j.OutputPath)
		}
		_ = os.Remove(filepath.Join(m.dir, j.ID+".mp4"))
		_ = m.st.DeleteExport(j.ID)
	}
}

func drawtextVF(name string, startEpochSec int64) string {
	n := sanitizeDrawtext(name)
	nameF := fmt.Sprintf(
		"drawtext=fontfile=%s:text='%s':x=14:y=14:fontsize=20:fontcolor=white:box=1:boxcolor=black@0.45:boxborderw=8",
		fontFile, n)
	timeF := fmt.Sprintf(
		"drawtext=fontfile=%s:text='%%{pts\\:localtime\\:%d}':x=14:y=h-th-14:fontsize=22:fontcolor=white:box=1:boxcolor=black@0.45:boxborderw=8",
		fontFile, startEpochSec)
	return nameF + "," + timeF
}

func sanitizeDrawtext(s string) string {
	r := strings.NewReplacer(":", " ", "'", "", "\\", "", "%", "", "\n", " ")
	out := r.Replace(s)
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 3, 64) }

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
