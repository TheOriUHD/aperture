// Package record runs continuous recording: one supervised ffmpeg per camera
// (stream-copy, no re-encode) pulling from go2rtc's restream, plus an indexer
// that registers completed segments in the database.
package record

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
)

const (
	segmentSeconds = 10
	indexInterval  = 8 * time.Second
	reconcileEvery = 60 * time.Second
)

// Recorder supervises per-camera ffmpeg processes and indexes their output.
type Recorder struct {
	cfg *config.Config
	st  *store.Store
	sm  *stream.Manager
	log *slog.Logger

	baseCtx context.Context
	mu      sync.Mutex
	active  map[string]context.CancelFunc // cameraID -> stop

	lastIdx map[string]int64 // cameraID -> last indexed segment epoch (seconds)
}

// New constructs a Recorder.
func New(cfg *config.Config, st *store.Store, sm *stream.Manager, log *slog.Logger) *Recorder {
	return &Recorder{
		cfg: cfg, st: st, sm: sm, log: log,
		active:  map[string]context.CancelFunc{},
		lastIdx: map[string]int64{},
	}
}

// Run starts the recorder: an initial reconcile plus the index and reconcile
// loops. It returns when ctx is cancelled.
func (r *Recorder) Run(ctx context.Context) {
	r.baseCtx = ctx
	r.Reconcile()

	indexT := time.NewTicker(indexInterval)
	reconT := time.NewTicker(reconcileEvery)
	defer indexT.Stop()
	defer reconT.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-indexT.C:
			r.indexOnce()
		case <-reconT.C:
			r.Reconcile()
		}
	}
}

func (r *Recorder) shouldRecord(c *store.Camera) bool {
	if !c.Enabled || c.RTSPMainURL == "" {
		return false
	}
	switch c.RecordMode {
	case store.ModeNever:
		return false
	case store.ModeScheduled:
		return r.scheduledActive(c.ID)
	default:
		// continuous + motion both capture to disk (motion is pruned to events
		// by the retention janitor).
		return true
	}
}

// scheduledActive reports whether the current local time falls inside one of a
// camera's recording windows. Reconcile re-checks every minute.
func (r *Recorder) scheduledActive(cameraID string) bool {
	scheds, err := r.st.ListSchedules(cameraID)
	if err != nil || len(scheds) == 0 {
		return false
	}
	now := time.Now()
	wd := int(now.Weekday())
	mins := now.Hour()*60 + now.Minute()
	for _, s := range scheds {
		if s.Weekday == wd && mins >= s.StartMin && mins < s.EndMin {
			return true
		}
	}
	return false
}

// Reconcile starts/stops per-camera recorders to match desired state.
func (r *Recorder) Reconcile() {
	if r.baseCtx == nil {
		return
	}
	cams, err := r.st.ListCameras()
	if err != nil {
		r.log.Error("recorder reconcile: list cameras", "err", err)
		return
	}
	desired := map[string]*store.Camera{}
	for _, c := range cams {
		if r.shouldRecord(c) {
			desired[c.ID] = c
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for id, cancel := range r.active {
		if _, ok := desired[id]; !ok {
			cancel()
			delete(r.active, id)
			r.log.Info("recorder stopped", "camera", id)
		}
	}
	for id, cam := range desired {
		if _, ok := r.active[id]; ok {
			continue
		}
		cctx, cancel := context.WithCancel(r.baseCtx)
		r.active[id] = cancel
		c := *cam
		go r.superviseFFmpeg(cctx, c)
		r.log.Info("recorder started", "camera", id, "name", cam.Name)
	}
}

// superviseFFmpeg keeps an ffmpeg recording process alive for one camera,
// restarting with backoff on unexpected exit.
func (r *Recorder) superviseFFmpeg(ctx context.Context, cam store.Camera) {
	dir := r.camDir(cam.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.log.Error("recorder mkdir", "camera", cam.ID, "err", err)
		return
	}
	backoff := time.Second
	for ctx.Err() == nil {
		args := []string{
			"-hide_banner", "-loglevel", "warning", "-nostdin",
			"-rtsp_transport", "tcp", "-fflags", "+genpts",
			"-i", r.sm.RTSPURL(cam.ID),
			"-c", "copy", "-f", "segment",
			"-segment_time", strconv.Itoa(segmentSeconds),
			"-segment_format", "mpegts",
			// No -reset_timestamps: keep PTS continuous within a recording
			// session so the generated HLS playlist plays smoothly; real gaps
			// (restarts) are emitted as EXT-X-DISCONTINUITY by the API.
			"-strftime", "1",
			filepath.Join(dir, "%s.ts"),
		}
		cmd := exec.CommandContext(ctx, "ffmpeg", args...)
		start := time.Now()
		err := cmd.Run()
		if ctx.Err() != nil {
			return // stopped intentionally
		}
		ran := time.Since(start)
		r.log.Warn("recorder ffmpeg exited; restarting", "camera", cam.ID, "ran", ran.Round(time.Second), "err", err)
		if ran > 30*time.Second {
			backoff = time.Second
		} else {
			backoff = min(backoff*2, 30*time.Second)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// indexOnce scans each camera's directory and indexes any completed,
// not-yet-indexed segment files.
func (r *Recorder) indexOnce() {
	cams, err := r.st.ListCameras()
	if err != nil {
		return
	}
	now := time.Now()
	for _, cam := range cams {
		dir := r.camDir(cam.ID)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		// Seed the per-camera cursor from the DB on first sight.
		if _, ok := r.lastIdx[cam.ID]; !ok {
			if maxMs, err := r.st.MaxSegmentStart(cam.ID); err == nil {
				r.lastIdx[cam.ID] = maxMs / 1000
			}
		}

		type segFile struct {
			epoch int64
			name  string
			mtime time.Time
			size  int64
		}
		var files []segFile
		var maxEpoch int64
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".ts") {
				continue
			}
			epoch, err := strconv.ParseInt(strings.TrimSuffix(e.Name(), ".ts"), 10, 64)
			if err != nil {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, segFile{epoch, e.Name(), info.ModTime(), info.Size()})
			if epoch > maxEpoch {
				maxEpoch = epoch
			}
		}

		last := r.lastIdx[cam.ID]
		for _, f := range files {
			if f.epoch <= last {
				continue
			}
			// A segment is complete once a newer file exists, or it has gone
			// quiet (recording paused/stopped).
			complete := f.epoch < maxEpoch || now.Sub(f.mtime) > 30*time.Second
			if !complete {
				continue
			}
			path := filepath.Join(dir, f.name)
			dur := probeDuration(path)
			if dur <= 0 {
				// Skip unreadable files; abandon only if they have gone stale.
				if now.Sub(f.mtime) > 2*time.Minute {
					r.log.Warn("recorder: unreadable segment, skipping", "path", path)
					r.lastIdx[cam.ID] = f.epoch
				}
				continue
			}
			startMs := f.epoch * 1000
			seg := &store.Segment{
				CameraID:   cam.ID,
				Path:       path,
				StartTS:    startMs,
				EndTS:      startMs + int64(dur*1000),
				DurationMS: int64(dur * 1000),
				SizeBytes:  f.size,
			}
			if err := r.st.InsertSegment(seg); err != nil {
				r.log.Error("recorder: index segment", "path", path, "err", err)
				continue
			}
			r.lastIdx[cam.ID] = f.epoch
		}
	}
}

func (r *Recorder) camDir(cameraID string) string {
	return filepath.Join(r.cfg.RecordingsDir, cameraID)
}

// probeDuration returns a media file's duration in seconds, or 0 on failure.
func probeDuration(path string) float64 {
	out, err := exec.Command("ffprobe", "-v", "error",
		"-show_entries", "format=duration", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0
	}
	return d
}
