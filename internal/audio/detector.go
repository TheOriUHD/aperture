// Package audio detects loud sounds per camera and records them as "audio"
// events, mirroring the motion detector's structure: one supervised ffmpeg per
// enabled camera, decoding the RTSP audio track to PCM, computing a normalized
// RMS loudness per window, and opening/closing events with pre/post-roll +
// cooldown. Opt-in per camera. No audio pipeline existed before this.
package audio

import (
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"math"
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
	sampleRate     = 8000 // mono PCM sample rate for analysis (audio, not playback)
	windowSamples  = 1600 // 200ms analysis windows
	defaultThresh  = 0.06 // normalized RMS (0..1) that trips an event by default
	prerollMs      = 3000
	postrollMs     = 4000
	cooldownMs     = 4000 // silence gap that closes an event
	reconcileEvery = 10 * time.Second
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
	maxLevel float64
}

// Detector supervises per-camera audio-level detection.
type Detector struct {
	cfg *config.Config
	st  *store.Store
	sm  snapshotter
	log *slog.Logger

	baseCtx context.Context
	mu      sync.Mutex
	active  map[string]context.CancelFunc
	sigs    map[string]string // cameraID -> threshold signature (restart-on-change)

	evMu    sync.Mutex
	events  map[string]*activeEvent
	onEvent      func(*store.Event)
	onEventClose func(string) // notified (event id) when an audio event finalizes
}

// New constructs an audio Detector.
func New(cfg *config.Config, st *store.Store, sm snapshotter, log *slog.Logger) *Detector {
	return &Detector{
		cfg: cfg, st: st, sm: sm, log: log,
		active: map[string]context.CancelFunc{},
		sigs:   map[string]string{},
		events: map[string]*activeEvent{},
	}
}

// SetOnEvent registers a callback invoked (async) when a new audio event starts
// — the same hook motion uses, so notify/automation react to audio too.
func (d *Detector) SetOnEvent(fn func(*store.Event)) { d.onEvent = fn }

// SetOnEventClose registers a callback invoked (async) with the event id when an
// audio event finalizes — emits the change feed's event.closed (parity with
// motion). No structural change: the audio detector already uses this pattern.
func (d *Detector) SetOnEventClose(fn func(string)) { d.onEventClose = fn }

// Run starts detection and the reconcile + finalize loops until ctx is done.
func (d *Detector) Run(ctx context.Context) {
	d.baseCtx = ctx
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
	return c.Enabled && c.RTSPMainURL != "" && c.AudioDetection && c.RecordMode != store.ModeNever
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
			delete(d.sigs, id)
		}
	}
	for id, cam := range want {
		sig := strconv.FormatFloat(cam.AudioThreshold, 'f', 4, 64)
		if _, ok := d.active[id]; ok {
			if d.sigs[id] == sig {
				continue
			}
			d.active[id]() // threshold changed — restart
			delete(d.active, id)
		}
		cctx, cancel := context.WithCancel(d.baseCtx)
		d.active[id] = cancel
		d.sigs[id] = sig
		c := *cam
		go d.detectCamera(cctx, c)
		d.log.Info("audio detection started", "camera", id, "threshold", cam.AudioThreshold)
	}
}

// detectCamera keeps an audio-decode ffmpeg alive and analyses its PCM output.
func (d *Detector) detectCamera(ctx context.Context, cam store.Camera) {
	thresh := cam.AudioThreshold
	if thresh <= 0 {
		thresh = defaultThresh
	}
	buf := make([]byte, windowSamples*2) // s16 = 2 bytes/sample
	backoff := time.Second
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, "ffmpeg",
			"-hide_banner", "-loglevel", "error", "-nostdin",
			"-rtsp_transport", "tcp", "-i", d.sm.RTSPURL(cam.ID),
			"-vn", "-ac", "1", "-ar", strconv.Itoa(sampleRate),
			"-f", "s16le", "-acodec", "pcm_s16le", "-")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return
		}
		if err := cmd.Start(); err != nil {
			d.log.Warn("audio ffmpeg start failed", "camera", cam.ID, "err", err)
		} else {
			start := time.Now()
			for {
				if _, err := io.ReadFull(stdout, buf); err != nil {
					break
				}
				if level := rms(buf); level > thresh {
					d.onTick(cam.ID, level)
				}
			}
			_ = cmd.Wait()
			if ctx.Err() != nil {
				return
			}
			if time.Since(start) > 30*time.Second {
				backoff = time.Second // it ran a while; reset backoff
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

// rms returns the normalized (0..1) RMS level of a little-endian s16 mono window.
func rms(buf []byte) float64 {
	n := len(buf) / 2
	if n == 0 {
		return 0
	}
	var sumSq float64
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(buf[i*2:])))
		sumSq += v * v
	}
	return math.Sqrt(sumSq/float64(n)) / 32768.0
}

// onTick records a loud window, starting an audio event if none is active.
func (d *Detector) onTick(cameraID string, level float64) {
	now := time.Now().UnixMilli()
	d.evMu.Lock()
	ev := d.events[cameraID]
	if ev == nil {
		ev = &activeEvent{id: store.NewID(), start: now - prerollMs, lastTick: now, maxLevel: level}
		d.events[cameraID] = ev
		d.evMu.Unlock()
		e := &store.Event{ID: ev.id, CameraID: cameraID, Type: "audio", StartTS: ev.start, Score: level}
		if err := d.st.InsertEvent(e); err != nil {
			d.log.Error("audio: insert event", "err", err)
			return
		}
		go d.captureThumb(cameraID, ev.id)
		if d.onEvent != nil {
			go d.onEvent(e)
		}
		return
	}
	ev.lastTick = now
	if level > ev.maxLevel {
		ev.maxLevel = level
	}
	d.evMu.Unlock()
}

// finalizeStale closes audio events whose sound has stopped (+postroll).
func (d *Detector) finalizeStale() {
	now := time.Now().UnixMilli()
	d.evMu.Lock()
	var done []*activeEvent
	for camID, ev := range d.events {
		if now-ev.lastTick > cooldownMs {
			done = append(done, ev)
			delete(d.events, camID)
		}
	}
	d.evMu.Unlock()
	for _, ev := range done {
		end := ev.lastTick + postrollMs
		if err := d.st.FinalizeEvent(ev.id, end, ev.maxLevel, nil); err != nil {
			d.log.Error("audio: finalize event", "err", err)
		} else if d.onEventClose != nil {
			go d.onEventClose(ev.id)
		}
	}
}

// captureThumb grabs a frame for the audio event so it has a preview.
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
	_ = d.st.SetEventThumb(eventID, path)
}
