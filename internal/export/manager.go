// Package export turns a camera + time range into a downloadable MP4. With no
// burn-in it is a near-instant stream-copy concat of the recorded MPEG-TS
// segments (honoring the no-transcode record path); burn-in is an explicit,
// bounded transcode. Jobs run one at a time through a worker so a large export
// never starves recording.
package export

import (
	"archive/zip"
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

	"github.com/TheOriUHD/aperture/internal/storage"
	"github.com/TheOriUHD/aperture/internal/store"
)

const (
	exportTTL = 24 * time.Hour
	fontFile  = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
)

// Manager owns the export output directory and the single-worker job queue.
type Manager struct {
	st    *store.Store
	reg   *storage.Registry
	dir   string
	log   *slog.Logger
	queue chan string
}

func New(st *store.Store, reg *storage.Registry, dir string, log *slog.Logger) *Manager {
	_ = os.MkdirAll(dir, 0o755)
	return &Manager{st: st, reg: reg, dir: dir, log: log, queue: make(chan string, 64)}
}

// localizeSegments returns a local filesystem path for each segment plus a
// cleanup func. Local segments (absolute path) are used in place; network
// segments (backend-relative path) are downloaded into temp files under the
// export dir so ffmpeg can concat them, and cleanup removes those temps.
func (m *Manager) localizeSegments(segs []*store.Segment) ([]string, func(), error) {
	paths := make([]string, len(segs))
	var temps []string
	cleanup := func() {
		for _, t := range temps {
			_ = os.Remove(t)
		}
	}
	for i, sg := range segs {
		if sg.Path != "" && filepath.IsAbs(sg.Path) {
			paths[i] = sg.Path
			continue
		}
		if sg.Path == "" || m.reg == nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("footage is unavailable")
		}
		backend, err := m.reg.For(sg.LocationID)
		if err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("storage backend unavailable")
		}
		rc, _, err := backend.Open(sg.Path)
		if err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("could not read footage")
		}
		tmp, err := os.CreateTemp(m.dir, "seg-*.ts")
		if err != nil {
			_ = rc.Close()
			cleanup()
			return nil, func() {}, err
		}
		_, cErr := io.Copy(tmp, rc)
		_ = rc.Close()
		_ = tmp.Close()
		if cErr != nil {
			_ = os.Remove(tmp.Name())
			cleanup()
			return nil, func() {}, cErr
		}
		temps = append(temps, tmp.Name())
		paths[i] = tmp.Name()
	}
	return paths, cleanup, nil
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

	cams := job.Cameras()
	if len(cams) == 0 {
		fail("no camera selected")
		return
	}
	names := map[string]string{}
	for _, cid := range cams {
		if c, err := m.st.GetCamera(cid); err == nil {
			names[cid] = c.Name
		} else {
			names[cid] = cid
		}
	}

	var out string
	switch {
	case job.Mode == "composite" && len(cams) >= 2:
		out = filepath.Join(m.dir, id+".mp4")
		if err := m.composite(ctx, job, cams, names, out); err != nil {
			fail(err.Error())
			return
		}
	case len(cams) > 1:
		out = filepath.Join(m.dir, id+".zip")
		if err := m.zipClips(ctx, id, job, cams, names, out); err != nil {
			fail(err.Error())
			return
		}
	default:
		out = filepath.Join(m.dir, id+".mp4")
		if err := m.clipForCamera(ctx, id, job, cams[0], names[cams[0]], out, true); err != nil {
			fail(err.Error())
			return
		}
	}

	fi, err := os.Stat(out)
	if err != nil {
		fail("export produced no file")
		return
	}
	_ = m.st.FinishExport(id, out, fi.Size(), time.Now().Unix())
	m.log.Info("export done", "id", id, "bytes", fi.Size(), "mode", job.Mode, "cameras", len(cams))
}

// clipForCamera renders one camera's window to a single MP4 (fast stream-copy,
// or a burn-in transcode). trackProg wires ffmpeg progress for the single-job UI.
func (m *Manager) clipForCamera(ctx context.Context, id string, job *store.ExportJob, cameraID, cameraName, out string, trackProg bool) error {
	segs, err := m.st.ListSegments(cameraID, job.FromTS, job.ToTS)
	if err != nil {
		return fmt.Errorf("could not list footage")
	}
	if len(segs) == 0 {
		return fmt.Errorf("no footage for %s in the selected range", cameraName)
	}

	localPaths, release, err := m.localizeSegments(segs)
	if err != nil {
		return err
	}
	defer release()

	listPath := filepath.Join(m.dir, id+"-"+cameraID+".txt")
	var b strings.Builder
	for _, p := range localPaths {
		b.WriteString("file '")
		b.WriteString(p)
		b.WriteString("'\n")
	}
	if err := os.WriteFile(listPath, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("internal error")
	}
	defer os.Remove(listPath)

	offset := float64(job.FromTS-segs[0].StartTS) / 1000.0
	if offset < 0 {
		offset = 0
	}
	durMs := job.ToTS - job.FromTS
	dur := float64(durMs) / 1000.0

	// A timelapse (speed > 1) can't be stream-copied — retiming needs a re-encode,
	// exactly like a burn-in. Either forces the libx264 path; both can combine.
	speed := job.Speed
	if speed < 1 {
		speed = 1
	}
	transcode := job.BurnIn || speed > 1

	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if transcode && trackProg {
		args = append(args, "-progress", "pipe:1")
	}
	args = append(args, "-f", "concat", "-safe", "0", "-i", listPath, "-ss", ftoa(offset), "-t", ftoa(dur))
	if transcode {
		var vf []string
		if speed > 1 {
			vf = append(vf, fmt.Sprintf("setpts=PTS/%d", speed))
		}
		if job.BurnIn {
			vf = append(vf, drawtextVF(cameraName, job.FromTS/1000))
		}
		if len(vf) > 0 {
			args = append(args, "-vf", strings.Join(vf, ","))
		}
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "22")
		if job.Audio && speed == 1 {
			args = append(args, "-c:a", "aac")
		} else {
			args = append(args, "-an") // timelapse audio is meaningless once retimed
		}
	} else {
		args = append(args, "-c", "copy")
		if !job.Audio {
			args = append(args, "-an")
		}
	}
	args = append(args, "-movflags", "+faststart", "-y", out)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if transcode && trackProg && durMs > 0 {
		outMs := durMs / int64(speed) // output is 1/speed as long as the input
		if stdout, err := cmd.StdoutPipe(); err == nil {
			go m.trackProgress(id, stdout, outMs)
		}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := firstLine(stderr.String())
		if msg == "" {
			msg = "ffmpeg failed"
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// zipClips renders one clip per camera and archives them into a .zip. Cameras
// with no footage in the window are skipped rather than failing the whole job.
func (m *Manager) zipClips(ctx context.Context, id string, job *store.ExportJob, cams []string, names map[string]string, out string) error {
	type clip struct{ path, name string }
	var clips []clip
	var tmp []string
	defer func() {
		for _, f := range tmp {
			_ = os.Remove(f)
		}
	}()
	for i, cid := range cams {
		cp := filepath.Join(m.dir, fmt.Sprintf("%s-%d.mp4", id, i))
		if err := m.clipForCamera(ctx, id, job, cid, names[cid], cp, false); err != nil {
			m.log.Warn("export: clip skipped", "camera", names[cid], "err", err)
			continue
		}
		tmp = append(tmp, cp)
		clips = append(clips, clip{cp, names[cid]})
		_ = m.st.SetExportProgress(id, float64(i+1)/float64(len(cams))*0.9)
	}
	if len(clips) == 0 {
		return fmt.Errorf("no footage in the selected range")
	}

	zf, err := os.Create(out)
	if err != nil {
		return fmt.Errorf("internal error")
	}
	defer zf.Close()
	zw := zip.NewWriter(zf)
	stamp := time.UnixMilli(job.FromTS).Format("2006-01-02_15-04-05")
	used := map[string]bool{}
	for _, c := range clips {
		name := sanitizeFile(c.name) + "_" + stamp + ".mp4"
		for used[name] {
			name = "x" + name
		}
		used[name] = true
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		src, err := os.Open(c.path)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, src)
		_ = src.Close()
		if err != nil {
			return err
		}
	}
	return zw.Close()
}

// composite renders 2–4 cameras into one multi-view MP4 via xstack (always a
// transcode; audio dropped). Inputs are time-aligned by seeking each to the
// window start.
func (m *Manager) composite(ctx context.Context, job *store.ExportJob, cams []string, names map[string]string, out string) error {
	if len(cams) > 4 {
		m.log.Warn("export: composite capped at 4 cameras", "requested", len(cams))
		cams = cams[:4]
	}
	var lists []string
	defer func() {
		for _, f := range lists {
			_ = os.Remove(f)
		}
	}()
	durMs := job.ToTS - job.FromTS
	dur := float64(durMs) / 1000.0

	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-progress", "pipe:1"}
	for i, cid := range cams {
		segs, err := m.st.ListSegments(cid, job.FromTS, job.ToTS)
		if err != nil || len(segs) == 0 {
			return fmt.Errorf("no footage for %s in the selected range", names[cid])
		}
		localPaths, release, err := m.localizeSegments(segs)
		if err != nil {
			return err
		}
		defer release()

		lp := filepath.Join(m.dir, fmt.Sprintf("%s-comp-%d.txt", job.ID, i))
		var b strings.Builder
		for _, p := range localPaths {
			b.WriteString("file '")
			b.WriteString(p)
			b.WriteString("'\n")
		}
		if err := os.WriteFile(lp, []byte(b.String()), 0o600); err != nil {
			return fmt.Errorf("internal error")
		}
		lists = append(lists, lp)
		off := float64(job.FromTS-segs[0].StartTS) / 1000.0
		if off < 0 {
			off = 0
		}
		// Per-input -ss/-t bounds each concat input to exactly the window, so
		// ffmpeg never reads (or the filtergraph never buffers) beyond it.
		args = append(args, "-ss", ftoa(off), "-t", ftoa(dur), "-f", "concat", "-safe", "0", "-i", lp)
	}

	fc := compositeFilter(len(cams))
	// -r caps the montage framerate; the filter resets each input's PTS so
	// xstack aligns them at 0 instead of waiting on divergent timestamps.
	args = append(args, "-filter_complex", fc, "-map", "[v]", "-r", "15", "-t", ftoa(dur),
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-an", "-movflags", "+faststart", "-y", out)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if durMs > 0 {
		if stdout, err := cmd.StdoutPipe(); err == nil {
			go m.trackProgress(job.ID, stdout, durMs)
		}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := firstLine(stderr.String())
		if msg == "" {
			msg = "ffmpeg failed"
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// compositeFilter builds the filter_complex that scales each input and stacks
// them: 2 = side-by-side, 3 = one top + two bottom, 4 = quad. Output label [v].
func compositeFilter(n int) string {
	const p = "setpts=PTS-STARTPTS,fps=15,setsar=1" // reset PTS + normalize fps so xstack aligns
	switch n {
	case 2:
		return "[0:v]scale=960:540," + p + "[a];[1:v]scale=960:540," + p + "[b];[a][b]hstack=inputs=2[v]"
	case 3:
		return "[0:v]scale=1920:540," + p + "[t];[1:v]scale=960:540," + p + "[bl];[2:v]scale=960:540," + p + "[br];" +
			"[t][bl][br]xstack=inputs=3:layout=0_0|0_h0|w1_h0[v]"
	default: // 4
		return "[0:v]scale=960:540," + p + "[a];[1:v]scale=960:540," + p + "[b];[2:v]scale=960:540," + p + "[c];[3:v]scale=960:540," + p + "[d];" +
			"[a][b][c][d]xstack=inputs=4:layout=0_0|w0_0|0_h0|w0_h0[v]"
	}
}

func sanitizeFile(s string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", " ", "_", ":", "-", "'", "", "\"", "")
	out := r.Replace(strings.TrimSpace(s))
	if out == "" {
		out = "camera"
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
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
		_ = os.Remove(filepath.Join(m.dir, j.ID+".zip"))
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
