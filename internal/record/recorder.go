// Package record runs continuous recording: one supervised ffmpeg per camera
// (stream-copy, no re-encode) pulling from go2rtc's restream, plus an indexer
// that registers completed segments in the database.
package record

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/storage"
	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
)

const (
	segmentSeconds = 10
	indexInterval  = 8 * time.Second
	reconcileEvery = 60 * time.Second
	// maxSegmentBytes caps how large a single, still-growing segment may get before
	// the recorder force-restarts the camera to cut it. A normal 10s segment is a
	// few MB; anything this large means the stream isn't rotating (no cut points),
	// which would otherwise grow unbounded and escape retention.
	maxSegmentBytes = 256 << 20 // 256 MiB
)

// Recorder supervises per-camera ffmpeg processes and indexes their output.
type Recorder struct {
	cfg *config.Config
	st  *store.Store
	sm  *stream.Manager
	reg *storage.Registry
	log *slog.Logger

	baseCtx context.Context
	mu      sync.Mutex
	active  map[string]context.CancelFunc // cameraID -> stop

	lastIdx map[string]int64 // cameraID -> last indexed segment epoch (seconds)
}

// New constructs a Recorder.
func New(cfg *config.Config, st *store.Store, sm *stream.Manager, reg *storage.Registry, log *slog.Logger) *Recorder {
	return &Recorder{
		cfg: cfg, st: st, sm: sm, reg: reg, log: log,
		active:  map[string]context.CancelFunc{},
		lastIdx: map[string]int64{},
	}
}

// Run starts the recorder: an initial reconcile plus the index and reconcile
// loops. It returns when ctx is cancelled.
func (r *Recorder) Run(ctx context.Context) {
	r.baseCtx = ctx
	r.Reconcile()
	r.uploadOnce() // drain any spool left from a previous run

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
			r.uploadOnce()
		case <-reconT.C:
			r.Reconcile()
		}
	}
}

// preferSubRecording reports the global recording-quality preference: true means
// "prefer LQ" (record each camera's sub stream when it has one). Default is HQ.
func (r *Recorder) preferSubRecording() bool {
	v, _ := r.st.GetSetting("record_quality")
	return v == "low"
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
	dir, _, _ := r.resolveTarget(cam)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.log.Error("recorder mkdir", "camera", cam.ID, "err", err)
		return
	}
	backoff := time.Second
	for ctx.Err() == nil {
		// Source stream: honor the global recording-quality preference. "Prefer LQ"
		// records the sub stream, but only for cameras that actually have one — it's
		// a preference, not a requirement, so single-stream cameras still record.
		src := r.sm.RTSPURL(cam.ID)
		if r.preferSubRecording() && cam.RTSPSubURL != "" {
			src = r.sm.RTSPURLSub(cam.ID)
		}
		args := []string{
			"-hide_banner", "-loglevel", "warning", "-nostdin",
			"-rtsp_transport", "tcp", "-fflags", "+genpts",
			"-i", src,
			// Record a single video + (optional) audio track. Some sources expose
			// several video tracks (e.g. a dual-lens/"main+sub" feed); copying them
			// all yields an mpegts the segment muxer can't rotate (one ever-growing
			// file). Mapping the first video track keeps segmenting reliable.
			"-map", "0:v:0", "-map", "0:a?",
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
	var stuckCams []string // cameras whose active segment won't rotate (see below)
	for _, cam := range cams {
		dir, locID, remote := r.resolveTarget(*cam)
		if remote {
			continue // network cameras are handled by the uploader
		}
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

		// Safety net: if the newest file is still being written but has grown far
		// past a normal segment, the stream has no usable cut points (e.g. a
		// multi-video or keyframe-less source), so ffmpeg never rotates. Left alone
		// it grows unbounded and — never being indexed — escapes retention and can
		// fill the disk. Flag the camera for a restart, which closes the file so it
		// gets indexed and becomes prunable.
		for _, f := range files {
			if f.epoch == maxEpoch && f.size > maxSegmentBytes && now.Sub(f.mtime) < 30*time.Second {
				stuckCams = append(stuckCams, cam.ID)
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
				LocationID: locID,
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

	// Restart any camera whose active segment grew past the cap without rotating,
	// so its file is closed + indexed and can't keep filling the disk.
	for _, id := range stuckCams {
		r.log.Warn("recorder: active segment not rotating; restarting camera to cap file growth", "camera", id, "capBytes", maxSegmentBytes)
		r.RestartCamera(id)
	}
}

// spoolMaxBytesPerCam bounds how much un-uploaded footage a single network camera
// may accumulate locally while its share is unreachable. Past this, the oldest
// spooled segments are dropped (with a warning) so a long outage can't fill
// DataDir — ffmpeg keeps recording the newest footage regardless.
const spoolMaxBytesPerCam = 4 << 30 // 4 GiB (~2h at 4 Mbps)

// uploadOnce drains each network camera's local spool to its backend: complete
// segments are uploaded, indexed, then deleted from the spool. A file is removed
// only after a successful upload+index, so a crash or network drop never loses
// footage — it is retried on the next tick (uploads are idempotent by path).
func (r *Recorder) uploadOnce() {
	if r.reg == nil {
		return
	}
	cams, err := r.st.ListCameras()
	if err != nil {
		return
	}
	now := time.Now()
	for _, cam := range cams {
		dir, locID, remote := r.resolveTarget(*cam)
		if !remote {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // nothing spooled yet
		}

		type spoolFile struct {
			epoch int64
			name  string
			mtime time.Time
			size  int64
		}
		var files []spoolFile
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
			files = append(files, spoolFile{epoch, e.Name(), info.ModTime(), info.Size()})
			if epoch > maxEpoch {
				maxEpoch = epoch
			}
		}
		sort.Slice(files, func(i, j int) bool { return files[i].epoch < files[j].epoch })

		backend, berr := r.reg.For(locID)
		for _, f := range files {
			// Complete once a newer file exists or it has gone quiet.
			if !(f.epoch < maxEpoch || now.Sub(f.mtime) > 30*time.Second) {
				continue
			}
			spoolPath := filepath.Join(dir, f.name)
			dur := probeDuration(spoolPath)
			if dur <= 0 {
				if now.Sub(f.mtime) > 2*time.Minute {
					r.log.Warn("recorder: unreadable spool segment, dropping", "path", spoolPath)
					_ = os.Remove(spoolPath)
				}
				continue
			}
			if berr != nil {
				// Backend unavailable — leave everything spooled and retry later.
				r.log.Warn("recorder: storage backend unavailable, spooling", "camera", cam.ID, "err", berr)
				break
			}
			rel := cam.ID + "/" + f.name
			n, err := backend.Put(spoolPath, rel)
			if err != nil {
				r.log.Warn("recorder: upload failed, will retry", "camera", cam.ID, "file", f.name, "err", err)
				break // backpressure: stop this camera this cycle
			}
			startMs := f.epoch * 1000
			seg := &store.Segment{
				CameraID:   cam.ID,
				LocationID: locID,
				Path:       rel,
				StartTS:    startMs,
				EndTS:      startMs + int64(dur*1000),
				DurationMS: int64(dur * 1000),
				SizeBytes:  n,
			}
			if err := r.st.InsertSegment(seg); err != nil {
				// Uploaded but not indexed — keep the spool file; the next Put to the
				// same rel simply overwrites, so a retry is safe.
				r.log.Error("recorder: index uploaded segment", "path", rel, "err", err)
				continue
			}
			_ = os.Remove(spoolPath)
		}
		r.enforceSpoolCap(cam.ID, dir)
	}
}

// enforceSpoolCap drops the oldest un-uploaded segments once a camera's spool
// exceeds spoolMaxBytesPerCam (a share has been down long enough to threaten
// DataDir). Newest footage is kept.
func (r *Recorder) enforceSpoolCap(cameraID, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type spoolFile struct {
		epoch int64
		name  string
		size  int64
	}
	var files []spoolFile
	var total int64
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
		files = append(files, spoolFile{epoch, e.Name(), info.Size()})
		total += info.Size()
	}
	if total <= spoolMaxBytesPerCam {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].epoch < files[j].epoch })
	dropped := 0
	for _, f := range files {
		if total <= spoolMaxBytesPerCam {
			break
		}
		if err := os.Remove(filepath.Join(dir, f.name)); err == nil {
			total -= f.size
			dropped++
		}
	}
	if dropped > 0 {
		r.log.Warn("recorder: spool over cap, dropped oldest un-uploaded segments",
			"camera", cameraID, "dropped", dropped)
	}
}

// ActiveCount returns how many cameras currently have a supervised recorder.
func (r *Recorder) ActiveCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.active)
}

// Reindex scans every camera's recordings directory and indexes any on-disk
// segment file that the database doesn't know about. It recovers footage that
// became orphaned (e.g. a DB/disk mismatch) so it shows up in playback and can
// be managed/pruned by storage. Durations are estimated from the gap to the
// next segment (segments are fixed-length) to avoid ffprobing tens of thousands
// of files; this is approximate but cheap and self-correcting.
func (r *Recorder) Reindex() (int, error) {
	cams, err := r.st.ListCameras()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, cam := range cams {
		dir, locID, remote := r.resolveTarget(*cam)
		if remote {
			continue // network footage is indexed by the uploader, not re-scanned here
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // no directory yet for this camera
		}
		have, err := r.st.SegmentStarts(cam.ID)
		if err != nil {
			return total, err
		}

		type segFile struct {
			epoch int64
			name  string
			size  int64
		}
		files := make([]segFile, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".ts") {
				continue
			}
			ep, err := strconv.ParseInt(strings.TrimSuffix(e.Name(), ".ts"), 10, 64)
			if err != nil {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, segFile{ep, e.Name(), info.Size()})
		}
		sort.Slice(files, func(i, j int) bool { return files[i].epoch < files[j].epoch })

		var add []*store.Segment
		for i, f := range files {
			startMs := f.epoch * 1000
			if have[startMs] {
				continue
			}
			dur := int64(segmentSeconds)
			if i+1 < len(files) {
				if gap := files[i+1].epoch - f.epoch; gap >= 1 && gap <= segmentSeconds*2 {
					dur = gap
				}
			}
			add = append(add, &store.Segment{
				CameraID:   cam.ID,
				LocationID: locID,
				Path:       filepath.Join(dir, f.name),
				StartTS:    startMs,
				EndTS:      startMs + dur*1000,
				DurationMS: dur * 1000,
				SizeBytes:  f.size,
			})
		}
		if err := r.st.InsertSegmentsBatch(add); err != nil {
			return total, fmt.Errorf("reindex %s: %w", cam.ID, err)
		}
		total += len(add)
		if len(add) > 0 {
			r.log.Info("reindex: recovered orphaned segments", "camera", cam.ID, "name", cam.Name, "count", len(add))
		}
	}
	return total, nil
}

// activeDir is the directory new recordings are written to for the built-in
// default location: the operator override if set, otherwise the deploy default.
func (r *Recorder) activeDir() string {
	if o := r.st.RecordingsPathOverride(); o != "" {
		return o
	}
	return r.cfg.RecordingsDir
}

// resolveTarget decides where a camera's ffmpeg writes and how the result is
// indexed:
//   - default / local locations → write straight to the location's directory and
//     index with an absolute path (remote=false).
//   - network locations (SMB/SFTP) → write to the local spool; the uploader picks
//     the files up and indexes them with a backend-relative path (remote=true).
//
// Unknown, disabled, or mis-configured locations fall back to the default dir so
// recording never stops, and the segment is indexed as living on the default.
func (r *Recorder) resolveTarget(cam store.Camera) (dir, locID string, remote bool) {
	def := func() (string, string, bool) { return filepath.Join(r.activeDir(), cam.ID), store.DefaultLocationID, false }
	if cam.LocationID == store.DefaultLocationID {
		return def()
	}
	loc, err := r.st.GetStorageLocation(cam.LocationID)
	if err != nil || !loc.Enabled {
		return def()
	}
	switch loc.Type {
	case store.LocationLocal:
		p, _ := loc.Config["path"].(string)
		if p == "" {
			return def()
		}
		return filepath.Join(p, cam.ID), cam.LocationID, false
	case store.LocationSMB, store.LocationSFTP:
		return filepath.Join(r.cfg.SpoolDir(), cam.ID), cam.LocationID, true
	default:
		return def()
	}
}

// RestartAll stops and restarts every active recorder, so a recordings-path
// change takes effect immediately (new ffmpeg processes pick up the new dir).
func (r *Recorder) RestartAll() {
	r.mu.Lock()
	for id, cancel := range r.active {
		cancel()
		delete(r.active, id)
	}
	r.mu.Unlock()
	r.Reconcile()
}

// RestartCamera stops one camera's recorder (if running) and re-reconciles so it
// restarts against its current config — used when a camera's storage location
// changes so ffmpeg begins writing to the new directory immediately.
func (r *Recorder) RestartCamera(id string) {
	r.mu.Lock()
	if cancel, ok := r.active[id]; ok {
		cancel()
		delete(r.active, id)
	}
	r.mu.Unlock()
	// The indexer cursor (lastIdx) is owned by the Run loop and needs no reset:
	// new segments in the new directory carry later epochs than the old cursor, so
	// they are indexed normally.
	r.Reconcile()
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
