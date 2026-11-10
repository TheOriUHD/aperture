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
	"syscall"
	"time"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/site"
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

	// durationSlack is how far a probed duration may exceed the time a file was
	// being written before it stops being believed.
	durationSlack = 5 // seconds

	// scheduleStartGrace is how long a recording window may be open before a camera
	// without new footage counts as failing: up to a minute for Reconcile to notice
	// the window, then ffmpeg connecting and the first segment landing and uploading.
	scheduleStartGrace = 3 * time.Minute

	// ffmpegStopGrace is how long a recording ffmpeg gets to finish after it is
	// asked to stop (SIGTERM) before it is killed. Finishing means flushing the
	// muxer and closing the segment it is writing, normally well under a second.
	ffmpegStopGrace = 10 * time.Second
)

// Recorder supervises per-camera ffmpeg processes and indexes their output.
type Recorder struct {
	cfg *config.Config
	st  *store.Store
	sm  *stream.Manager
	reg *storage.Registry
	log *slog.Logger

	// runCam records one camera until its context ends; superviseFFmpeg, except
	// in tests.
	runCam func(ctx context.Context, cam store.Camera)

	mu      sync.Mutex
	baseCtx context.Context
	active  map[string]*camProc // cameraID -> the supervisor meant to be recording
	// exiting holds, per camera, the newest supervisor that was stopped and may
	// still be finishing its segment. A new one for the same camera waits for it:
	// two ffmpegs must never write one directory, as both could open the same
	// second's file.
	exiting map[string]*camProc
	// procs counts every supervisor still running, stopped or not; Run waits for
	// it on shutdown so each ffmpeg has closed its segment before nvrd exits.
	procs sync.WaitGroup

	lastIdx map[string]int64 // cameraID -> last indexed segment epoch (seconds)
}

// camProc is one camera's recording supervisor.
type camProc struct {
	stop context.CancelFunc
	done chan struct{} // closed once its ffmpeg has exited and it has returned
}

// New constructs a Recorder.
func New(cfg *config.Config, st *store.Store, sm *stream.Manager, reg *storage.Registry, log *slog.Logger) *Recorder {
	r := &Recorder{
		cfg: cfg, st: st, sm: sm, reg: reg, log: log,
		active:  map[string]*camProc{},
		exiting: map[string]*camProc{},
		lastIdx: map[string]int64{},
	}
	r.runCam = r.superviseFFmpeg
	return r
}

// Run starts the recorder: an initial reconcile plus the index and reconcile
// loops. When ctx is cancelled it stops every camera and returns only once each
// ffmpeg has finished the segment it was writing (or been killed after
// ffmpegStopGrace), so a caller that waits for Run loses no footage on shutdown.
func (r *Recorder) Run(ctx context.Context) {
	r.mu.Lock()
	r.baseCtx = ctx
	r.mu.Unlock()
	r.Reconcile()
	r.uploadOnce() // drain any spool left from a previous run

	indexT := time.NewTicker(indexInterval)
	reconT := time.NewTicker(reconcileEvery)
	defer indexT.Stop()
	defer reconT.Stop()
	for {
		select {
		case <-ctx.Done():
			r.drain()
			return
		case <-indexT.C:
			r.indexOnce()
			r.uploadOnce()
			r.mirrorOnce()
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

// ShouldRecord reports whether a camera is meant to be recording right now.
func (r *Recorder) ShouldRecord(c *store.Camera) bool {
	if !c.Enabled || c.RTSPMainURL == "" {
		return false
	}
	switch c.RecordMode {
	case store.ModeNever:
		return false
	case store.ModeScheduled:
		return r.scheduledActive(c.ID, time.Now())
	default:
		// continuous + motion both capture to disk (motion is pruned to events
		// by the retention janitor).
		return true
	}
}

// RecordingDue reports whether footage should be arriving from a camera by now.
// Everything that calls a camera broken goes through this, not ShouldRecord: a
// camera outside its window has old footage on purpose, and at the moment a window
// opens its newest footage is hours old too.
func (r *Recorder) RecordingDue(c *store.Camera) bool {
	return r.recordingDueAt(c, time.Now())
}

func (r *Recorder) recordingDueAt(c *store.Camera, now time.Time) bool {
	if !c.Enabled || c.RecordMode == store.ModeNever {
		return false
	}
	if c.RecordMode != store.ModeScheduled {
		return true
	}
	return r.scheduledActive(c.ID, now) && r.scheduledActive(c.ID, now.Add(-scheduleStartGrace))
}

// scheduledActive reports whether now — in the site-profile timezone, not the
// host's — falls inside one of a camera's recording windows. Recording windows are
// authored against the site's wall clock, so a host TZ that differs from the site
// (or a DST transition) must not shift them. Reconcile re-checks every minute.
func (r *Recorder) scheduledActive(cameraID string, now time.Time) bool {
	scheds, err := r.st.ListSchedules(cameraID)
	if err != nil || len(scheds) == 0 {
		return false
	}
	if p := site.Load(r.st); p.Timezone != "" {
		if loc, err := time.LoadLocation(p.Timezone); err == nil {
			now = now.In(loc)
		}
	}
	return scheduleMatch(scheds, now)
}

// scheduleMatch reports whether now — evaluated in whatever location it carries —
// falls inside one of the recording windows. Kept pure (no clock, no store) so
// the timezone-sensitive matching is deterministically testable.
func scheduleMatch(scheds []*store.Schedule, now time.Time) bool {
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
	if !r.running() {
		return
	}
	cams, err := r.st.ListCameras()
	if err != nil {
		r.log.Error("recorder reconcile: list cameras", "err", err)
		return
	}
	desired := map[string]*store.Camera{}
	for _, c := range cams {
		if r.ShouldRecord(c) {
			desired[c.ID] = c
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Checked again under the lock: once Run has begun draining, starting a
	// supervisor would race its WaitGroup.Wait and outlive the shutdown.
	if r.baseCtx.Err() != nil {
		return
	}
	for id := range r.active {
		if _, ok := desired[id]; !ok {
			r.stopLocked(id)
			r.log.Info("recorder stopped", "camera", id)
		}
	}
	for id, cam := range desired {
		if _, ok := r.active[id]; ok {
			continue
		}
		r.startLocked(*cam)
		r.log.Info("recorder started", "camera", id, "name", cam.Name)
	}
}

// running reports whether Run has started and not yet been cancelled.
func (r *Recorder) running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.baseCtx != nil && r.baseCtx.Err() == nil
}

// startLocked starts a supervisor for cam. If a stopped one for the same camera
// is still finishing its segment, the new one waits for it before it records.
// Caller holds r.mu.
func (r *Recorder) startLocked(cam store.Camera) {
	ctx, cancel := context.WithCancel(r.baseCtx)
	p := &camProc{stop: cancel, done: make(chan struct{})}
	prev := r.exiting[cam.ID]
	r.active[cam.ID] = p
	r.procs.Add(1)
	go func() {
		defer r.procs.Done()
		defer r.exited(cam.ID, p)
		if prev != nil {
			<-prev.done // bounded: prev's ffmpeg is killed after ffmpegStopGrace
		}
		r.runCam(ctx, cam)
	}()
}

// stopLocked stops a camera's supervisor. Its ffmpeg finishes the current
// segment in the background. Caller holds r.mu.
func (r *Recorder) stopLocked(id string) {
	p, ok := r.active[id]
	if !ok {
		return
	}
	p.stop()
	delete(r.active, id)
	r.exiting[id] = p
}

// exited marks a supervisor as finished: its ffmpeg is gone.
func (r *Recorder) exited(id string, p *camProc) {
	close(p.done)
	r.mu.Lock()
	if r.exiting[id] == p {
		delete(r.exiting, id)
	}
	r.mu.Unlock()
}

// drain waits, after Run's context was cancelled, for every supervisor (and so
// every ffmpeg) to finish. Taking the lock first means any Reconcile still in
// flight has either already added its supervisor to procs or will see the
// cancelled context and start none.
func (r *Recorder) drain() {
	r.mu.Lock()
	n := len(r.active) + len(r.exiting)
	r.mu.Unlock()
	r.procs.Wait()
	if n > 0 {
		r.log.Info("recorder: all cameras stopped, segments closed", "cameras", n)
	}
}

// inputArgs opens a camera's restream for recording. Timestamps come from the clock,
// not the stream: go2rtc can reconnect a camera under a running ffmpeg (a stream
// restart, a dropped link) and the camera's RTP clock comes back hours away. The
// segment muxer cuts on those timestamps, so it then splits at every keyframe or
// never, and audio and video end up hours apart in the file. A re-encoding recorder
// also caps its decoder threads (quality.go), since only it decodes at all.
func inputArgs(src string, reencode bool) []string {
	args := []string{"-rtsp_transport", "tcp", "-use_wallclock_as_timestamps", "1", "-fflags", "+genpts"}
	if reencode {
		args = append(args, "-threads", reencodeThreads)
	}
	return append(args, "-i", src)
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
	reencode, quickExits := wantsReencode(cam), 0
	for ctx.Err() == nil {
		// Source stream: honor the global recording-quality preference. "Prefer LQ"
		// records the sub stream, but only for cameras that actually have one — it's
		// a preference, not a requirement, so single-stream cameras still record.
		src := r.sm.RTSPURL(cam.ID)
		if r.preferSubRecording() && cam.RTSPSubURL != "" {
			src = r.sm.RTSPURLSub(cam.ID)
		}
		args := []string{"-hide_banner", "-loglevel", "warning", "-nostdin"}
		args = append(args, inputArgs(src, reencode)...)
		args = append(args,
			// Record a single video + (optional) audio track. Some sources expose
			// several video tracks (e.g. a dual-lens/"main+sub" feed); copying them
			// all yields an mpegts the segment muxer can't rotate (one ever-growing
			// file). Mapping the first video track keeps segmenting reliable.
			"-map", "0:v:0", "-map", "0:a?",
		)
		// Video is stream-copied — the recording path stays transcode-free and
		// near-zero CPU, which is the whole design — unless this camera opted into
		// a lower recording quality (quality.go).
		args = append(args, videoArgs(cam, reencode)...)
		args = append(args,
			// Audio is NOT copied, and this is deliberate. ffmpeg 7.x announces the
			// AAC track in the PMT and then writes zero packets to it when copying
			// AAC out of RTSP into mpegts: the PID exists, carries no PES at all,
			// and every downstream reader sees a declared track with sample_rate=0.
			// ffmpeg 5.x wrote proper ADTS. The switch to the distro ffmpeg
			// (47cb6f3) therefore silenced every recording from 2026-08-20 21:13
			// onward, with no error anywhere — ffprobe still prints "aac".
			// Re-encoding is the only thing that restores it (verified against the
			// live restream: copy -> aac,0,0; re-encode -> aac,16000,1). At 16 kHz
			// mono this costs a rounding error of CPU. Revert to "-c", "copy" only
			// after confirming a future ffmpeg writes audio packets again.
			"-c:a", "aac", "-b:a", "32k",
			"-f", "segment",
			"-segment_time", strconv.Itoa(segmentSeconds),
			"-segment_format", "mpegts",
			// No -reset_timestamps: keep PTS continuous within a recording
			// session so the generated HLS playlist plays smoothly; real gaps
			// (restarts) are emitted as EXT-X-DISCONTINUITY by the API.
			"-strftime", "1",
			filepath.Join(dir, "%s.ts"),
		)
		cmd := recordCmd(ctx, args)
		start := time.Now()
		err := cmd.Run()
		if ctx.Err() != nil {
			return // stopped intentionally
		}
		ran := time.Since(start)
		r.log.Warn("recorder ffmpeg exited; restarting", "camera", cam.ID, "ran", ran.Round(time.Second), "err", err)
		if reencode && !keepReencoding(ran, &quickExits) {
			reencode = false
			r.log.Error("recorder: re-encoding keeps failing, recording the original stream instead",
				"camera", cam.ID, "name", cam.Name)
		}
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

// recordCmd builds a recording ffmpeg that, when ctx ends (shutdown, or the
// camera stopped or restarted), is asked to finish rather than killed. On SIGTERM
// ffmpeg stops reading, flushes its muxer and closes the segment it is writing,
// so the footage up to the stop is on disk in full. exec.CommandContext's default,
// SIGKILL, dropped whatever it still had buffered. An ffmpeg that has not exited
// ffmpegStopGrace after the signal is killed.
func recordCmd(ctx context.Context, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = ffmpegStopGrace
	return cmd
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

		for _, f := range files {
			if f.epoch == maxEpoch && notRotating(f.size, f.mtime, now) {
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
			dur := writtenDuration(probeDuration(path), f.epoch, f.mtime)
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

	r.restartStuck(stuckCams)
}

// notRotating reports that the newest file, still being written, has grown far past
// a normal segment: the stream has no usable cut points (a multi-video or
// keyframe-less source, or timestamps that jumped backwards), so ffmpeg never
// rotates. Left alone it grows unbounded and, never being indexed or uploaded,
// escapes retention and can fill the disk.
func notRotating(size int64, mtime, now time.Time) bool {
	return size > maxSegmentBytes && now.Sub(mtime) < 30*time.Second
}

// restartStuck restarts cameras whose active segment grew past the cap without
// rotating, which closes the file so it gets indexed and becomes prunable.
func (r *Recorder) restartStuck(ids []string) {
	for _, id := range ids {
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
	var stuckCams []string // cameras whose active segment won't rotate
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

		for _, f := range files {
			if f.epoch == maxEpoch && notRotating(f.size, f.mtime, now) {
				stuckCams = append(stuckCams, cam.ID)
			}
		}

		backend, berr := r.reg.For(locID)
		for _, f := range files {
			// Complete once a newer file exists or it has gone quiet.
			if !(f.epoch < maxEpoch || now.Sub(f.mtime) > 30*time.Second) {
				continue
			}
			spoolPath := filepath.Join(dir, f.name)
			dur := writtenDuration(probeDuration(spoolPath), f.epoch, f.mtime)
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

	r.restartStuck(stuckCams)
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
	def := func() (string, string, bool) {
		return filepath.Join(r.activeDir(), cam.ID), store.DefaultLocationID, false
	}
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
	for id := range r.active {
		r.stopLocked(id)
	}
	r.mu.Unlock()
	r.Reconcile()
}

// RestartCamera stops one camera's recorder (if running) and re-reconciles so it
// restarts against its current config — used when a camera's storage location
// changes so ffmpeg begins writing to the new directory immediately.
func (r *Recorder) RestartCamera(id string) {
	r.mu.Lock()
	r.stopLocked(id)
	r.mu.Unlock()
	// The indexer cursor (lastIdx) is owned by the Run loop and needs no reset:
	// new segments in the new directory carry later epochs than the old cursor, so
	// they are indexed normally.
	r.Reconcile()
}

// NeedsRecovery reports whether a camera whose footage has stopped arriving has
// really stopped recording. It hasn't if ffmpeg is still writing: that footage is
// only waiting on an upload, and restarting the stream under it does harm (see the
// watchdog). Nor has a camera that isn't meant to be recording right now.
func (r *Recorder) NeedsRecovery(c *store.Camera) bool {
	if !r.ShouldRecord(c) {
		return false
	}
	dir, _, _ := r.resolveTarget(*c)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".ts") {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) < 30*time.Second {
			return false
		}
	}
	return true
}

// writtenDuration caps a probed duration at how long the file was actually being
// written. A timestamp jump inside a segment probes as hours of video, and an end
// time in the future both breaks playback and hides the camera from the watchdog.
func writtenDuration(probed float64, epoch int64, mtime time.Time) float64 {
	written := mtime.Sub(time.Unix(epoch, 0)).Seconds()
	if written > 0 && probed > written+durationSlack {
		return written
	}
	return probed
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
