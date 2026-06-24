// Package motion runs cheap, no-AI motion detection: one supervised ffmpeg per
// camera decodes a downscaled, low-fps copy of the stream and emits scene-change
// "ticks" above the camera's threshold. Ticks are aggregated into events with
// pre/post-roll, and each event gets a thumbnail.
package motion

import (
	"bufio"
	"context"
	"fmt"
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

	evMu    sync.Mutex
	events  map[string]*activeEvent // cameraID -> ongoing event
	onEvent func(*store.Event)      // notified when a new event opens
}

// SetOnEvent registers a callback invoked (async) when a new event starts.
func (d *Detector) SetOnEvent(fn func(*store.Event)) { d.onEvent = fn }

// New constructs a Detector.
func New(cfg *config.Config, st *store.Store, sm snapshotter, log *slog.Logger) *Detector {
	return &Detector{
		cfg: cfg, st: st, sm: sm, log: log,
		active: map[string]context.CancelFunc{},
		events: map[string]*activeEvent{},
	}
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

	d.mu.Lock()
	defer d.mu.Unlock()
	for id, cancel := range d.active {
		if _, ok := want[id]; !ok {
			cancel()
			delete(d.active, id)
		}
	}
	for id, cam := range want {
		if _, ok := d.active[id]; ok {
			continue
		}
		cctx, cancel := context.WithCancel(d.baseCtx)
		d.active[id] = cancel
		c := *cam
		go d.detectCamera(cctx, c)
		d.log.Info("motion detection started", "camera", id, "threshold", cam.MotionThreshold)
	}
}

// detectCamera keeps a motion ffmpeg alive and parses its scene-score output.
func (d *Detector) detectCamera(ctx context.Context, cam store.Camera) {
	threshold := cam.MotionThreshold
	if threshold <= 0 {
		threshold = 0.02
	}
	backoff := time.Second
	for ctx.Err() == nil {
		vf := fmt.Sprintf("fps=4,scale=480:-2,select='gt(scene,%s)',metadata=print",
			strconv.FormatFloat(threshold, 'f', 4, 64))
		// metadata=print (no file=) writes via ffmpeg's logger to stderr, which
		// flushes per line in real time — unlike file=- which uses buffered avio
		// and only flushes on exit. -nostats keeps the progress line out.
		cmd := exec.CommandContext(ctx, "ffmpeg",
			"-hide_banner", "-loglevel", "info", "-nostats", "-nostdin",
			"-rtsp_transport", "tcp", "-i", d.sm.RTSPURL(cam.ID),
			"-an", "-vf", vf, "-f", "null", "-")
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return
		}
		if err := cmd.Start(); err != nil {
			d.log.Warn("motion ffmpeg start failed", "camera", cam.ID, "err", err)
		} else {
			start := time.Now()
			sc := bufio.NewScanner(stderr)
			for sc.Scan() {
				line := sc.Text()
				if i := strings.Index(line, "lavfi.scene_score="); i >= 0 {
					score, _ := strconv.ParseFloat(strings.TrimSpace(line[i+len("lavfi.scene_score="):]), 64)
					d.onTick(cam.ID, score)
				}
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
		ev = &activeEvent{id: store.NewID(), start: now - prerollMs, lastTick: now, maxScore: score}
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
		if now-ev.lastTick > cooldown.Milliseconds() {
			done = append(done, ev)
			delete(d.events, camID)
		}
	}
	d.evMu.Unlock()
	for _, ev := range done {
		if err := d.st.FinalizeEvent(ev.id, ev.lastTick+postrollMs, ev.maxScore); err != nil {
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
