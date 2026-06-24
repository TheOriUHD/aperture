// Package motion runs cheap, no-AI motion detection: one supervised ffmpeg per
// camera decodes a downscaled, low-fps copy of the stream and emits scene-change
// "ticks" above the camera's threshold. Ticks are aggregated into events with
// pre/post-roll, and each event gets a thumbnail.
package motion

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

const (
	prerollMs      = 5000
	postrollMs     = 5000
	cooldown       = 5 * time.Second
	reconcileEvery = 60 * time.Second
)

// snapshotter is the subset of the stream manager the detector needs.
type snapshotter interface {
	RTSPURL(cameraID string) string
	SnapshotCached(ctx context.Context, cameraID string) ([]byte, error)
}

type activeEvent struct {
	id       string
	start    int64 // unix ms (incl. preroll)
	lastTick int64 // unix ms
	maxScore float64
	postroll int64
	cooldown int64
	minDur   int64
}

// Detector supervises per-camera motion detection.
type Detector struct {
	cfg *config.Config
	st  *store.Store
	sm  snapshotter
	log *slog.Logger

	baseCtx context.Context
	mu      sync.Mutex
	active  map[string]context.CancelFunc
	sigs    map[string]string // cameraID -> settings signature (for restart-on-change)

	tuneMu sync.Mutex
	tune   map[string]camTune // cameraID -> event-lifecycle tuning

	evMu    sync.Mutex
	events  map[string]*activeEvent // cameraID -> ongoing event
	onEvent func(*store.Event)      // notified when a new event opens
}

// camTune is a camera's per-event timing, captured at event open.
type camTune struct {
	preroll  int64
	postroll int64
	cooldown int64
	minDur   int64
}

// SetOnEvent registers a callback invoked (async) when a new event starts.
func (d *Detector) SetOnEvent(fn func(*store.Event)) { d.onEvent = fn }

// New constructs a Detector.
func New(cfg *config.Config, st *store.Store, sm snapshotter, log *slog.Logger) *Detector {
	return &Detector{
		cfg: cfg, st: st, sm: sm, log: log,
		active: map[string]context.CancelFunc{},
		sigs:   map[string]string{},
		tune:   map[string]camTune{},
		events: map[string]*activeEvent{},
	}
}

func tuneOf(c *store.Camera) camTune {
	t := camTune{
		preroll:  int64(c.PrerollMs),
		postroll: int64(c.PostrollMs),
		cooldown: int64(c.CooldownMs),
		minDur:   int64(c.MinDurationMs),
	}
	if t.preroll <= 0 {
		t.preroll = prerollMs
	}
	if t.postroll <= 0 {
		t.postroll = postrollMs
	}
	if t.cooldown <= 0 {
		t.cooldown = cooldown.Milliseconds()
	}
	return t
}

func (d *Detector) tuneFor(cameraID string) camTune {
	d.tuneMu.Lock()
	defer d.tuneMu.Unlock()
	if t, ok := d.tune[cameraID]; ok {
		return t
	}
	return camTune{preroll: prerollMs, postroll: postrollMs, cooldown: cooldown.Milliseconds()}
}

// Run starts detection and the reconcile + finalize loops until ctx is done.
func (d *Detector) Run(ctx context.Context) {
	d.baseCtx = ctx
	if err := d.st.FinalizeOrphanedEvents(); err != nil {
		d.log.Error("motion: finalize orphaned events", "err", err)
	}
	d.Reconcile()

	recon := time.NewTicker(reconcileEvery)
	fin := time.NewTicker(time.Second)
	defer recon.Stop()
	defer fin.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-recon.C:
			d.Reconcile()
		case <-fin.C:
			d.finalizeStale()
		}
	}
}

func (d *Detector) shouldDetect(c *store.Camera) bool {
	return c.Enabled && c.RTSPMainURL != "" && c.RecordMode != store.ModeNever
}

// Reconcile starts/stops detectors to match the desired camera set.
func (d *Detector) Reconcile() {
	if d.baseCtx == nil {
		return
	}
	cams, err := d.st.ListCameras()
	if err != nil {
		return
	}
	want := map[string]*store.Camera{}
	for _, c := range cams {
		if d.shouldDetect(c) {
			want[c.ID] = c
		}
	}

	// Refresh the event-lifecycle tuning cache (applies on the next event;
	// no ffmpeg restart needed since roll/cooldown don't affect detection).
	d.tuneMu.Lock()
	nt := make(map[string]camTune, len(want))
	for id, c := range want {
		nt[id] = tuneOf(c)
	}
	d.tune = nt
	d.tuneMu.Unlock()

	d.mu.Lock()
	defer d.mu.Unlock()
	for id, cancel := range d.active {
		if _, ok := want[id]; !ok {
			cancel()
			delete(d.active, id)
			delete(d.sigs, id)
		}
	}
	for id, cam := range want {
		sig := strconv.FormatFloat(cam.MotionThreshold, 'f', 4, 64) + "|" + cam.MotionZonesJSON
		if _, ok := d.active[id]; ok {
			if d.sigs[id] == sig {
				continue
			}
			// Detection settings changed — restart with the new threshold.
			d.active[id]()
			delete(d.active, id)
		}
		cctx, cancel := context.WithCancel(d.baseCtx)
		d.active[id] = cancel
		d.sigs[id] = sig
		c := *cam
		go d.detectCamera(cctx, c)
		d.log.Info("motion detection started", "camera", id, "threshold", cam.MotionThreshold)
	}
}

// detectCamera keeps a motion ffmpeg alive and parses its scene-score output.
// detectCamera streams a tiny grayscale copy of the camera and computes
// region-aware motion in Go: consecutive frames are diffed cell-by-cell,
// ignore-masked cells are skipped, and the fraction of changed cells is compared
// to the camera's sensitivity. This replaces the whole-frame scene-change scalar
// so that motion zones (and, later, object gating) have spatial information.
func (d *Detector) detectCamera(ctx context.Context, cam store.Camera) {
	threshold := cam.MotionThreshold
	if threshold <= 0 {
		threshold = 0.02
	}
	mask := parseIgnoreMask(cam.MotionZonesJSON)
	active := gridW*gridH - countTrue(mask)
	if active <= 0 {
		active = 1
	}
	const frameSize = gridW * gridH
	backoff := time.Second
	for ctx.Err() == nil {
		vf := fmt.Sprintf("fps=5,scale=%d:%d,format=gray", gridW, gridH)
		cmd := exec.CommandContext(ctx, "ffmpeg",
			"-hide_banner", "-loglevel", "error", "-nostdin",
			"-rtsp_transport", "tcp", "-i", d.sm.RTSPURL(cam.ID),
			"-an", "-vf", vf, "-f", "rawvideo", "-pix_fmt", "gray", "-")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return
		}
		if err := cmd.Start(); err != nil {
			d.log.Warn("motion ffmpeg start failed", "camera", cam.ID, "err", err)
		} else {
			start := time.Now()
			prev := make([]byte, frameSize)
			cur := make([]byte, frameSize)
			havePrev := false
			for {
				if _, err := io.ReadFull(stdout, cur); err != nil {
					break
				}
				if havePrev {
					if frac := diffFrac(prev, cur, mask, active); frac > threshold {
						d.onTick(cam.ID, frac)
					}
				}
				prev, cur = cur, prev
				havePrev = true
			}
			_ = cmd.Wait()
			if ctx.Err() != nil {
				return
			}
			if time.Since(start) > 30*time.Second {
				backoff = time.Second
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// onTick records a motion frame, starting an event if none is active.
func (d *Detector) onTick(cameraID string, score float64) {
	now := time.Now().UnixMilli()
	d.evMu.Lock()
	ev := d.events[cameraID]
	if ev == nil {
		t := d.tuneFor(cameraID)
		ev = &activeEvent{
			id: store.NewID(), start: now - t.preroll, lastTick: now, maxScore: score,
			postroll: t.postroll, cooldown: t.cooldown, minDur: t.minDur,
		}
		d.events[cameraID] = ev
		d.evMu.Unlock()
		e := &store.Event{ID: ev.id, CameraID: cameraID, Type: "motion", StartTS: ev.start, Score: score}
		if err := d.st.InsertEvent(e); err != nil {
			d.log.Error("motion: insert event", "err", err)
			return
		}
		go d.captureThumb(cameraID, ev.id)
		if d.onEvent != nil {
			go d.onEvent(e)
		}
		return
	}
	ev.lastTick = now
	if score > ev.maxScore {
		ev.maxScore = score
	}
	d.evMu.Unlock()
}

// finalizeStale closes events whose motion has stopped (+postroll).
func (d *Detector) finalizeStale() {
	now := time.Now().UnixMilli()
	d.evMu.Lock()
	var done []*activeEvent
	for camID, ev := range d.events {
		cd := ev.cooldown
		if cd <= 0 {
			cd = cooldown.Milliseconds()
		}
		if now-ev.lastTick > cd {
			done = append(done, ev)
			delete(d.events, camID)
		}
	}
	d.evMu.Unlock()
	for _, ev := range done {
		end := ev.lastTick + ev.postroll
		// Min-duration filter: discard brief blips (compression artifacts,
		// a single-frame flash) instead of committing them as events.
		if ev.minDur > 0 && end-ev.start < ev.minDur {
			if e, err := d.st.EventByID(ev.id); err == nil && e.ThumbPath != "" {
				_ = os.Remove(e.ThumbPath)
			}
			_ = d.st.DeleteEvent(ev.id)
			continue
		}
		if err := d.st.FinalizeEvent(ev.id, end, ev.maxScore); err != nil {
			d.log.Error("motion: finalize event", "err", err)
		}
	}
}

// captureThumb grabs a frame for the event and stores it.
func (d *Detector) captureThumb(cameraID, eventID string) {
	ctx, cancel := context.WithTimeout(d.baseCtx, 8*time.Second)
	defer cancel()
	data, err := d.sm.SnapshotCached(ctx, cameraID)
	if err != nil {
		return
	}
	dir := filepath.Join(d.cfg.RecordingsDir, cameraID, "events")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, eventID+".jpg")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return
	}
	if err := d.st.SetEventThumb(eventID, path); err != nil {
		d.log.Error("motion: set thumb", "err", err)
	}
}
