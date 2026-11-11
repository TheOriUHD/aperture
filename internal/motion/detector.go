// Package motion runs cheap, no-AI motion detection: one supervised ffmpeg per
// camera decodes a downscaled, low-fps copy of the stream and emits scene-change
// "ticks" above the camera's threshold. Ticks are aggregated into events with
// pre/post-roll, and each event gets a thumbnail.
package motion

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	// motionHold is how long a camera counts as "in motion" after its last
	// motion frame, for the live outline and live object recognition.
	motionHold = 1500 * time.Millisecond
)

// FrameSink receives what object recognition needs from the motion pipeline:
// colour frames (decoded once, alongside the motion grid), each motion frame's
// coarse change map, and when a camera starts and stops moving.
type FrameSink interface {
	// FrameSize is the height colour frames should fit in for a camera (they
	// may be up to three times as wide, for panoramic cameras); 0 means the
	// camera needs none.
	FrameSize(cameraID string) int
	// FrameRate is how many colour frames a second to deliver (motion itself
	// always looks at 5).
	FrameRate(cameraID string) int
	// OnFrame gets each colour frame (packed RGB). rgb is reused afterwards.
	OnFrame(cameraID string, w, h int, rgb []byte)
	// OnMotion gets each motion frame's SearchW×SearchH change map. Reused afterwards.
	OnMotion(cameraID string, cells []bool)
	// OnMotionState gets a camera starting (true) or stopping (false) to move.
	OnMotionState(cameraID string, active bool)
}

// snapshotter is the subset of the stream manager the detector needs.
type snapshotter interface {
	RTSPURL(cameraID string) string
	RTSPURLSub(cameraID string) string
	SnapshotCached(ctx context.Context, cameraID string) ([]byte, error)
}

// motionSource is the stream motion detection decodes. Detection only ever looks
// at a 64x36 grid, so a camera with a sub stream is watched on that: decoding a 4K
// HEVC main stream for it cost over a core and about half a gigabyte per camera,
// enough to starve the whole service. Sub streams show the same picture as the main
// one (measured on the live cameras), so motion zones stay where they were drawn.
func motionSource(sm snapshotter, cam store.Camera) string {
	if cam.RTSPSubURL != "" {
		return sm.RTSPURLSub(cam.ID)
	}
	return sm.RTSPURL(cam.ID)
}

type activeEvent struct {
	id       string
	start    int64 // unix ms (incl. preroll)
	lastTick int64 // unix ms
	maxScore float64
	postroll int64
	cooldown int64
	minDur   int64
	cells    []bool // union of coarse motion cells across the event (SearchW*SearchH)
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
	cams    sync.WaitGroup    // per-camera detectors; Run waits for them (and their ffmpeg) on shutdown

	tuneMu sync.Mutex
	tune   map[string]camTune // cameraID -> event-lifecycle tuning

	sink   FrameSink       // object recognition, when wired (guarded by mu)
	moving map[string]bool // cameraID -> currently in motion (motion-state loop only)
	// liveTicks is the last motion frame of live-only cameras (recognition on,
	// nothing recorded): they get the live outline and boxes but no events.
	liveTicks map[string]int64

	evMu         sync.Mutex
	events       map[string]*activeEvent // cameraID -> ongoing event
	onEvent      func(*store.Event)      // notified when a new event opens
	onEventClose func(string)            // notified (event id) when an event finalizes
}

// camTune is a camera's per-event timing, captured at event open.
type camTune struct {
	preroll  int64
	postroll int64
	cooldown int64
	minDur   int64
}

// SetFrameSink wires object recognition in. Cameras restart their decoder on
// the next Reconcile when the frames they need change.
func (d *Detector) SetFrameSink(s FrameSink) {
	d.mu.Lock()
	d.sink = s
	d.mu.Unlock()
}

// Watching reports whether motion detection (and with it recognition) reads the
// camera.
func (d *Detector) Watching(cameraID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.active[cameraID]
	return ok
}

func (d *Detector) frameSink() FrameSink {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sink
}

// SetOnEvent registers a callback invoked (async) when a new event starts.
func (d *Detector) SetOnEvent(fn func(*store.Event)) { d.onEvent = fn }

// SetOnEventClose registers a callback invoked (async) with the event id when an
// event finalizes. Used to emit the change feed's event.closed.
func (d *Detector) SetOnEventClose(fn func(string)) { d.onEventClose = fn }

// New constructs a Detector.
func New(cfg *config.Config, st *store.Store, sm snapshotter, log *slog.Logger) *Detector {
	return &Detector{
		cfg: cfg, st: st, sm: sm, log: log,
		active:    map[string]context.CancelFunc{},
		sigs:      map[string]string{},
		tune:      map[string]camTune{},
		events:    map[string]*activeEvent{},
		moving:    map[string]bool{},
		liveTicks: map[string]int64{},
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
	state := time.NewTicker(250 * time.Millisecond)
	defer recon.Stop()
	defer fin.Stop()
	defer state.Stop()
	for {
		select {
		case <-ctx.Done():
			// Taking the lock first: a Reconcile in flight has either added its
			// detector to d.cams already or will see ctx done and start none.
			d.mu.Lock()
			d.mu.Unlock()
			d.cams.Wait() // each detector's ffmpeg is killed and reaped
			return
		case <-recon.C:
			d.Reconcile()
		case <-fin.C:
			d.finalizeStale()
		case <-state.C:
			d.motionStates()
		}
	}
}

func (d *Detector) shouldDetect(c *store.Camera) bool {
	// Motion-only recording inherently needs the motion trigger; otherwise honor
	// the per-camera toggle (continuous cameras can record without motion events).
	motionOn := c.MotionDetection || c.RecordMode == store.ModeMotion
	return c.Enabled && c.RTSPMainURL != "" && c.RecordMode != store.ModeNever && motionOn
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
	liveOnly := map[string]bool{}
	for _, c := range cams {
		if d.shouldDetect(c) {
			want[c.ID] = c
		} else if d.sink != nil && c.Enabled && c.RTSPMainURL != "" && d.sink.FrameSize(c.ID) > 0 {
			// Object recognition wants this camera although it records
			// nothing: watch motion for the live view only.
			want[c.ID] = c
			liveOnly[c.ID] = true
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
	if d.baseCtx.Err() != nil {
		return // shutting down: Run is waiting on d.cams
	}
	for id, cancel := range d.active {
		if _, ok := want[id]; !ok {
			cancel()
			delete(d.active, id)
			delete(d.sigs, id)
		}
	}
	for id, cam := range want {
		// The source is part of the signature, so adding or removing a sub stream
		// moves detection onto it.
		frames, rate := 0, 0
		if d.sink != nil {
			frames = d.sink.FrameSize(id)
			rate = d.sink.FrameRate(id)
		}
		sig := strconv.FormatFloat(cam.MotionThreshold, 'f', 4, 64) + "|" + cam.MotionZonesJSON + "|" + motionSource(d.sm, *cam) + "|" + strconv.Itoa(frames) + "@" + strconv.Itoa(rate) + "|" + strconv.FormatBool(liveOnly[id])
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
		sink := d.sink
		live := liveOnly[id]
		d.cams.Add(1)
		go func() {
			defer d.cams.Done()
			d.detectCamera(cctx, c, sink, frames, rate, live)
		}()
		d.log.Info("motion detection started", "camera", id, "threshold", cam.MotionThreshold, "recognitionFrames", frames, "liveOnly", live)
	}
}

// detectCamera keeps a motion ffmpeg alive and parses its scene-score output.
// detectCamera streams a tiny grayscale copy of the camera and computes
// region-aware motion in Go: consecutive frames are diffed cell-by-cell,
// ignore-masked cells are skipped, and the fraction of changed cells is compared
// to the camera's sensitivity. This replaces the whole-frame scene-change scalar
// so that motion zones (and, later, object gating) have spatial information.
//
// When object recognition wants frames for the camera, the same decode also
// produces colour frames (fitted within 3·frames×frames) on a second pipe, so
// recognition costs no extra decoding.
//
// A live-only camera (recognition on, recording off) creates no events: its
// motion only drives the live outline and live recognition.
func (d *Detector) detectCamera(ctx context.Context, cam store.Camera, sink FrameSink, frames, rate int, liveOnly bool) {
	if rate <= 0 {
		rate = 5
	}
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
		var cmd *exec.Cmd
		var colour *os.File
		if sink != nil && frames > 0 {
			fc := fmt.Sprintf("[0:v]split=2[a][b];[a]fps=5,scale=%d:%d,format=gray[m];"+
				"[b]fps=%d,scale=%d:%d:force_original_aspect_ratio=decrease:force_divisible_by=2,format=rgb24[c]", gridW, gridH, rate, 3*frames, frames)
			cmd = exec.CommandContext(ctx, "ffmpeg",
				"-hide_banner", "-loglevel", "info", "-nostats", "-nostdin",
				"-threads", "2", "-rtsp_transport", "tcp", "-i", motionSource(d.sm, cam), "-an",
				"-filter_complex", fc,
				"-map", "[m]", "-f", "rawvideo", "-pix_fmt", "gray", "pipe:1",
				"-map", "[c]", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:3")
			r, w, err := os.Pipe()
			if err != nil {
				return
			}
			colour = r
			cmd.ExtraFiles = []*os.File{w}
		} else {
			vf := fmt.Sprintf("fps=5,scale=%d:%d,format=gray", gridW, gridH)
			cmd = exec.CommandContext(ctx, "ffmpeg",
				"-hide_banner", "-loglevel", "error", "-nostdin",
				"-threads", "2", "-rtsp_transport", "tcp", "-i", motionSource(d.sm, cam),
				"-an", "-vf", vf, "-f", "rawvideo", "-pix_fmt", "gray", "-")
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return
		}
		var stderr io.ReadCloser
		if colour != nil {
			if stderr, err = cmd.StderrPipe(); err != nil {
				return
			}
		}
		err = cmd.Start()
		if colour != nil {
			cmd.ExtraFiles[0].Close() // the child holds the write end now
		}
		if err != nil {
			d.log.Warn("motion ffmpeg start failed", "camera", cam.ID, "err", err)
			if colour != nil {
				colour.Close()
			}
		} else {
			var colourDone chan struct{}
			if colour != nil {
				colourDone = make(chan struct{})
				size := make(chan [2]int, 1)
				go watchColourSize(stderr, size)
				go func() {
					defer close(colourDone)
					readColour(colour, size, func(w, h int, rgb []byte) { sink.OnFrame(cam.ID, w, h, rgb) })
				}()
			}
			start := time.Now()
			prev := make([]byte, frameSize)
			cur := make([]byte, frameSize)
			frameCells := make([]bool, SearchW*SearchH)
			havePrev := false
			for {
				if _, err := io.ReadFull(stdout, cur); err != nil {
					break
				}
				if havePrev {
					for i := range frameCells {
						frameCells[i] = false
					}
					if frac := diffFrac(prev, cur, mask, active, frameCells); frac > threshold {
						if liveOnly {
							d.evMu.Lock()
							d.liveTicks[cam.ID] = time.Now().UnixMilli()
							d.evMu.Unlock()
						} else {
							d.onTick(cam.ID, frac, frameCells)
						}
						if sink != nil {
							sink.OnMotion(cam.ID, frameCells)
						}
					}
				}
				prev, cur = cur, prev
				havePrev = true
			}
			_ = cmd.Wait()
			if colour != nil {
				colour.Close()
				<-colourDone
			}
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

// outSize matches ffmpeg's description of the colour output, e.g.
// "Stream #1:0: Video: rawvideo (RGB[24] / 0x18424752), rgb24(pc, …), 416x236, …".
var outSize = regexp.MustCompile(`, (\d{2,5})x(\d{2,5})[ ,]`)

// watchColourSize reads ffmpeg's log for the colour output's frame size and
// then keeps draining it.
func watchColourSize(stderr io.Reader, size chan<- [2]int) {
	sc := bufio.NewScanner(stderr)
	sent := false
	for sc.Scan() {
		line := sc.Text()
		if sent || !strings.Contains(line, "Stream #1:0") || !strings.Contains(line, "rgb24") {
			continue
		}
		if m := outSize.FindStringSubmatch(line); m != nil {
			w, _ := strconv.Atoi(m[1])
			h, _ := strconv.Atoi(m[2])
			size <- [2]int{w, h}
			sent = true
		}
	}
	if !sent {
		close(size)
	}
}

// readColour reads fixed-size RGB frames once their size is known, handing
// each to fn. It always drains the pipe so ffmpeg never blocks on it.
func readColour(r io.Reader, size <-chan [2]int, fn func(w, h int, rgb []byte)) {
	var wh [2]int
	var ok bool
	select {
	case wh, ok = <-size:
	case <-time.After(30 * time.Second):
	}
	if !ok || wh[0] <= 0 || wh[1] <= 0 {
		_, _ = io.Copy(io.Discard, r)
		return
	}
	buf := make([]byte, wh[0]*wh[1]*3)
	for {
		if _, err := io.ReadFull(r, buf); err != nil {
			return
		}
		fn(wh[0], wh[1], buf)
	}
}

// motionStates tells the sink when cameras start and stop moving.
func (d *Detector) motionStates() {
	sink := d.frameSink()
	if sink == nil {
		return
	}
	now := time.Now().UnixMilli()
	d.evMu.Lock()
	now2 := map[string]bool{}
	for camID, ev := range d.events {
		if now-ev.lastTick < motionHold.Milliseconds() {
			now2[camID] = true
		}
	}
	for camID, at := range d.liveTicks {
		if now-at < motionHold.Milliseconds() {
			now2[camID] = true
		} else if now-at > time.Minute.Milliseconds() {
			delete(d.liveTicks, camID)
		}
	}
	d.evMu.Unlock()
	var on, off []string
	for id := range now2 {
		if !d.moving[id] {
			on = append(on, id)
		}
	}
	for id := range d.moving {
		if !now2[id] {
			off = append(off, id)
		}
	}
	d.moving = now2
	for _, id := range on {
		sink.OnMotionState(id, true)
	}
	for _, id := range off {
		sink.OnMotionState(id, false)
	}
}

// onTick records a motion frame, starting an event if none is active. cells is
// the current frame's coarse motion map, unioned into the event for Grid Search.
func (d *Detector) onTick(cameraID string, score float64, cells []bool) {
	now := time.Now().UnixMilli()
	d.evMu.Lock()
	ev := d.events[cameraID]
	if ev == nil {
		t := d.tuneFor(cameraID)
		ev = &activeEvent{
			id: store.NewID(), start: now - t.preroll, lastTick: now, maxScore: score,
			postroll: t.postroll, cooldown: t.cooldown, minDur: t.minDur,
			cells: make([]bool, SearchW*SearchH),
		}
		orCells(ev.cells, cells)
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
	orCells(ev.cells, cells)
	d.evMu.Unlock()
}

// orCells unions src into dst (both SearchW*SearchH bool grids).
func orCells(dst, src []bool) {
	for i := range dst {
		if i < len(src) && src[i] {
			dst[i] = true
		}
	}
}

// cellIndices returns the sorted set of active cell indices in a search grid.
func cellIndices(cells []bool) []int {
	var out []int
	for i, c := range cells {
		if c {
			out = append(out, i)
		}
	}
	return out
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
		if err := d.st.FinalizeEvent(ev.id, end, ev.maxScore, cellIndices(ev.cells)); err != nil {
			d.log.Error("motion: finalize event", "err", err)
		} else if d.onEventClose != nil {
			go d.onEventClose(ev.id)
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
