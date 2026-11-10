package detect

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/TheOriUHD/aperture/internal/ai"
	"github.com/TheOriUHD/aperture/internal/motion"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Live is local object recognition on live video. The motion pipeline hands it
// colour frames and motion maps (it implements motion.FrameSink). While a
// camera moves, its latest frame goes through the model a few times a second;
// while someone watches it live, also when nothing moves, a little less often,
// so a person standing still in view is boxed too. Results go to the change
// feed for the live view, and to the camera's ongoing event.
//
// What counts:
//   - a person overlapping motion, who did within the last minute (someone
//     who walked in and sat down keeps their box), or who is recognised with
//     extra confidence even if they never moved;
//   - anything else (a car, a dog) once it has itself changed place, for a
//     minute after. Motion merely inside its box isn't enough: someone walking
//     past a parked car, or headlights sweeping over it, would box the car.
//
// A parked car, or a lamp post the model mistakes for a person, doesn't count.
type Live struct {
	e       *Engine
	ai      *ai.Manager
	publish func(typ, camera string, data any)

	mu      sync.Mutex
	cams    map[string]*liveCam
	wake    chan struct{}
	inUse   int   // frames in flight to the worker
	nextObj int64 // object ids handed out by the tracker
}

type liveCam struct {
	enabled bool // recognition is on for this camera
	viewers int  // live-view connections watching it right now

	frame   []byte
	w, h    int
	frameAt time.Time
	seq     uint64
	doneSeq uint64
	lastRun time.Time
	busy    bool

	moving bool
	cells  []timedCells // recent motion maps

	shown  bool // objects are on screen (so a clear is owed)
	parked int  // parked vehicles last reported

	tracks []tracked          // objects followed from frame to frame
	path   []store.TrackPoint // positions not yet written for the event
	pathAt time.Time          // when the path was last written

	event      string
	best       map[string]store.EventObject
	bestSig    string
	savedSig   string
	savedAt    time.Time
	firstFound chan struct{} // closed on the event's first object
}

// tracked is one object the tracker follows.
type tracked struct {
	id      int64
	label   string
	box     LiveObject
	seen    time.Time
	movedAt time.Time  // when it last moved (see acceptLocked)
	shownAt time.Time  // when it last counted (was boxed)
	anchor  LiveObject // where it was when first seen or when it last moved
	born    time.Time  // when the tracker first saw it
}

type timedCells struct {
	at    time.Time
	cells []bool
}

// LiveObject is one object as the live view draws it.
type LiveObject struct {
	ID    int64   `json:"id"` // the same object keeps its id from frame to frame
	Label string  `json:"label"`
	Score float64 `json:"score"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	W     float64 `json:"w"`
	H     float64 `json:"h"`
}

const (
	maxInFlight  = 2
	cellMemory   = 1500 * time.Millisecond
	saveInterval = 2 * time.Second
	trackMemory  = 3 * time.Second         // an unseen object is forgotten after this
	stickyFor    = time.Minute             // an object that moved stays boxed this long while seen
	stillBonus   = 0.1                     // extra confidence a person who never moved needs
	coastFor     = 1200 * time.Millisecond // a boxed object missed for a frame keeps its box this long
)

func newLive(e *Engine, m *ai.Manager, publish func(typ, camera string, data any)) *Live {
	l := &Live{e: e, ai: m, publish: publish, cams: map[string]*liveCam{}, wake: make(chan struct{}, 1)}
	return l
}

func (l *Live) cam(id string) *liveCam {
	c := l.cams[id]
	if c == nil {
		c = &liveCam{}
		l.cams[id] = c
	}
	return c
}

// setEnabled records which cameras use local recognition.
func (l *Live) setEnabled(on map[string]bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, c := range l.cams {
		if !on[id] {
			c.enabled = false
			c.frame = nil
		}
	}
	for id := range on {
		l.cam(id).enabled = true
	}
}

// FrameSize implements motion.FrameSink.
func (l *Live) FrameSize(cameraID string) int {
	l.mu.Lock()
	c := l.cams[cameraID]
	enabled := c != nil && c.enabled
	l.mu.Unlock()
	if !enabled {
		return 0
	}
	return l.ai.InputSize()
}

// FrameRate implements motion.FrameSink: a GPU keeps up with twice the frames,
// so boxes follow movement more closely.
func (l *Live) FrameRate(cameraID string) int {
	if l.ai.Provider() == "cuda" {
		return 10
	}
	return 5
}

// OnFrame implements motion.FrameSink: keep the latest frame of a moving camera.
func (l *Live) OnFrame(cameraID string, w, h int, rgb []byte) {
	if l.e.pkgs != nil {
		l.e.pkgs.offer(cameraID, w, h, rgb)
	}
	l.mu.Lock()
	c := l.cams[cameraID]
	if c == nil || !c.enabled || (!c.moving && c.viewers == 0) {
		l.mu.Unlock()
		return
	}
	if cap(c.frame) < len(rgb) {
		c.frame = make([]byte, len(rgb))
	}
	c.frame = c.frame[:len(rgb)]
	copy(c.frame, rgb)
	c.w, c.h = w, h
	c.frameAt = time.Now()
	c.seq++
	l.mu.Unlock()
	l.poke()
}

// OnMotion implements motion.FrameSink: remember where things moved.
func (l *Live) OnMotion(cameraID string, cells []bool) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.cams[cameraID]
	if c == nil || !c.enabled {
		return
	}
	keep := c.cells[:0]
	for _, tc := range c.cells {
		if now.Sub(tc.at) < cellMemory {
			keep = append(keep, tc)
		}
	}
	cp := make([]bool, len(cells))
	copy(cp, cells)
	c.cells = append(keep, timedCells{at: now, cells: cp})
}

// OnMotionState implements motion.FrameSink. It also drives the live motion
// outline, whether or not recognition is on.
func (l *Live) OnMotionState(cameraID string, active bool) {
	l.publish("motion.state", cameraID, map[string]any{"active": active})
	if l.e.pkgs != nil {
		l.e.pkgs.motion(cameraID, active)
	}
	l.mu.Lock()
	c := l.cam(cameraID)
	c.moving = active
	// A watched camera keeps recognising (and so keeps its boxes up to date);
	// an unwatched one drops everything when it stops moving.
	clear := !active && c.shown && c.viewers == 0
	if !active && c.viewers == 0 {
		c.frame = nil
		c.cells = nil
		c.shown = false
		c.tracks = nil
	}
	l.mu.Unlock()
	if clear {
		l.publish("objects.live", cameraID, map[string]any{"ts": time.Now().UnixMilli(), "objects": []LiveObject{}})
	}
}

func (l *Live) poke() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// run schedules frames to the worker, fairly across moving cameras.
func (l *Live) run(ctx context.Context) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-l.wake:
		case <-tick.C:
		}
		for {
			id, c, frame, w, h, at := l.next()
			if c == nil {
				break
			}
			go l.detect(ctx, id, frame, w, h, at)
		}
	}
}

// next picks the moving camera that waited longest for a fresh frame.
func (l *Live) next() (string, *liveCam, []byte, int, int, time.Time) {
	if !l.ai.Running() {
		return "", nil, nil, 0, 0, time.Time{}
	}
	interval := l.e.liveInterval()
	watchInterval := l.e.watchInterval()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inUse >= maxInFlight {
		return "", nil, nil, 0, 0, time.Time{}
	}
	now := time.Now()
	var pick string
	var pc *liveCam
	for id, c := range l.cams {
		gap := interval
		if !c.moving {
			gap = watchInterval
		}
		if !c.enabled || (!c.moving && c.viewers == 0) || c.busy || c.frame == nil || c.seq == c.doneSeq || now.Sub(c.lastRun) < gap {
			continue
		}
		if pc == nil || c.lastRun.Before(pc.lastRun) {
			pick, pc = id, c
		}
	}
	if pc == nil {
		return "", nil, nil, 0, 0, time.Time{}
	}
	pc.busy = true
	pc.lastRun = now
	pc.doneSeq = pc.seq
	l.inUse++
	frame := make([]byte, len(pc.frame))
	copy(frame, pc.frame)
	return pick, pc, frame, pc.w, pc.h, pc.frameAt
}

func (l *Live) detect(ctx context.Context, id string, frame []byte, w, h int, at time.Time) {
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	res, err := l.ai.Detect(dctx, frame, w, h)
	cancel()
	cfg := l.e.LoadConfig()

	l.mu.Lock()
	l.inUse--
	c := l.cams[id]
	if c == nil {
		l.mu.Unlock()
		return
	}
	c.busy = false
	if err != nil || (!c.moving && c.viewers == 0) {
		l.mu.Unlock()
		l.poke()
		return
	}
	motionMap := unionCells(c.cells, time.Now())
	cands := candidates(res.Boxes, cfg)
	l.trackLocked(c, cands, at)
	objs := acceptLocked(c, cands, motionMap, cfg, at)
	parked := parkedLocked(c, objs, at)
	publish := len(objs) > 0 || c.shown || parked > 0 || c.parked > 0
	c.shown = len(objs) > 0
	c.parked = parked
	saveEvent, eventID := l.aggregateLocked(c, objs)
	var best []store.EventObject
	if saveEvent {
		best = bestList(c.best)
	}
	path := l.pathLocked(c, objs, at)
	l.mu.Unlock()
	l.poke()

	if l.e.pkgs != nil {
		// Delivered parcels are drawn too, while they stand there.
		if pk := l.e.pkgs.liveObjects(id); len(pk) > 0 {
			objs = append(objs, pk...)
			publish = true
		}
	}
	if publish {
		l.publish("objects.live", id, map[string]any{"ts": at.UnixMilli(), "objects": objs, "parked": parked})
	}
	if saveEvent {
		l.e.saveObjects(eventID, id, best)
	}
	if len(path) > 0 {
		l.e.savePath(id, path)
	}
}

// trackLocked gives each object the id it had in earlier frames: the same
// label, overlapping or close by (people move between frames a few times a
// second apart), best scores first.
func (l *Live) trackLocked(c *liveCam, objs []LiveObject, at time.Time) {
	keep := c.tracks[:0]
	for _, t := range c.tracks {
		if at.Sub(t.seen) < trackMemory {
			keep = append(keep, t)
		}
	}
	c.tracks = keep
	order := make([]int, len(objs))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return objs[order[a]].Score > objs[order[b]].Score })
	used := map[int]bool{}
	for _, i := range order {
		o := &objs[i]
		best, bestScore := -1, 0.0
		for ti, t := range c.tracks {
			if used[ti] || t.label != o.Label {
				continue
			}
			s := matchScore(t.box, *o)
			if s > bestScore {
				best, bestScore = ti, s
			}
		}
		if best >= 0 {
			used[best] = true
			o.ID = c.tracks[best].id
			c.tracks[best].box, c.tracks[best].seen = *o, at
			continue
		}
		l.nextObj++
		o.ID = l.nextObj
		c.tracks = append(c.tracks, tracked{id: o.ID, label: o.Label, box: *o, seen: at, anchor: *o, born: at})
		used[len(c.tracks)-1] = true
	}
}

// matchScore is how likely two boxes are the same object: their overlap, or
// failing that how close their centres are relative to their size. 0 = no.
func matchScore(a, b LiveObject) float64 {
	iou := float64(ai.IoU(
		ai.Box{X: float32(a.X), Y: float32(a.Y), W: float32(a.W), H: float32(a.H)},
		ai.Box{X: float32(b.X), Y: float32(b.Y), W: float32(b.W), H: float32(b.H)}))
	if iou > 0.1 {
		return 1 + iou
	}
	dx := (a.X + a.W/2) - (b.X + b.W/2)
	dy := (a.Y + a.H/2) - (b.Y + b.H/2)
	reach := max(a.W, a.H, b.W, b.H)
	if d := dx*dx + dy*dy; d < reach*reach {
		return 1 - d/(reach*reach)
	}
	return 0
}

// pathLocked records where the objects are for the camera's event, and hands
// back a batch to write every couple of seconds.
func (l *Live) pathLocked(c *liveCam, objs []LiveObject, at time.Time) []store.TrackPoint {
	if c.event == "" {
		return nil
	}
	for _, o := range objs {
		c.path = append(c.path, store.TrackPoint{EventID: c.event, Obj: o.ID, Label: o.Label, Confidence: o.Score,
			X: o.X, Y: o.Y, W: o.W, H: o.H, TS: at.UnixMilli()})
	}
	if len(c.path) == 0 || time.Since(c.pathAt) < saveInterval {
		return nil
	}
	out := c.path
	c.path = nil
	c.pathAt = time.Now()
	return out
}

// aggregateLocked folds objects into the camera's ongoing event, keeping the
// best sighting per label. It says when the event's objects should be saved.
func (l *Live) aggregateLocked(c *liveCam, objs []LiveObject) (bool, string) {
	if c.event == "" || len(objs) == 0 {
		return false, ""
	}
	if c.best == nil {
		c.best = map[string]store.EventObject{}
	}
	now := time.Now().UnixMilli()
	for _, o := range objs {
		if cur, ok := c.best[o.Label]; !ok || o.Score > cur.Confidence {
			c.best[o.Label] = store.EventObject{Label: o.Label, Confidence: o.Score, X: o.X, Y: o.Y, W: o.W, H: o.H, TS: now}
		}
	}
	if c.firstFound != nil {
		close(c.firstFound)
		c.firstFound = nil
	}
	c.bestSig = sigOf(c.best)
	if c.bestSig != c.savedSig && time.Since(c.savedAt) >= saveInterval {
		c.savedSig, c.savedAt = c.bestSig, time.Now()
		return true, c.event
	}
	return false, ""
}

// sigOf changes when a label is added or its best score moves by 5 points.
func sigOf(best map[string]store.EventObject) string {
	keys := make([]string, 0, len(best))
	for k, o := range best {
		keys = append(keys, k+":"+string(rune('a'+int(o.Confidence*20))))
	}
	sort.Strings(keys)
	s := ""
	for _, k := range keys {
		s += k + ","
	}
	return s
}

func bestList(best map[string]store.EventObject) []store.EventObject {
	out := make([]store.EventObject, 0, len(best))
	for _, o := range best {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	return out
}

// eventOpened ties a new motion event to its camera.
func (l *Live) eventOpened(ev *store.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.cam(ev.CameraID)
	c.event = ev.ID
	c.best, c.bestSig, c.savedSig, c.savedAt = nil, "", "", time.Time{}
	c.firstFound = make(chan struct{})
}

// eventClosed saves anything not yet saved and forgets the event.
func (l *Live) eventClosed(eventID string) {
	l.mu.Lock()
	var camID string
	var best []store.EventObject
	var path []store.TrackPoint
	for id, c := range l.cams {
		if c.event != eventID {
			continue
		}
		camID = id
		if c.bestSig != "" && c.bestSig != c.savedSig {
			best = bestList(c.best)
		}
		path = c.path
		c.path = nil
		c.event, c.best, c.bestSig, c.savedSig = "", nil, "", ""
		c.firstFound = nil
	}
	l.mu.Unlock()
	if len(best) > 0 {
		l.e.saveObjects(eventID, camID, best)
	}
	if len(path) > 0 {
		l.e.savePath(camID, path)
	}
}

// waitFirst waits up to d for the first object of a camera's ongoing event and
// returns what was found by then.
func (l *Live) waitFirst(cameraID, eventID string, d time.Duration) []store.EventObject {
	l.mu.Lock()
	c := l.cams[cameraID]
	if c == nil || c.event != eventID || !c.enabled {
		l.mu.Unlock()
		return nil
	}
	ch := c.firstFound
	l.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		case <-time.After(d):
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if c.event != eventID || len(c.best) == 0 {
		return nil
	}
	c.savedSig, c.savedAt = c.bestSig, time.Now()
	return bestList(c.best)
}

// unionCells merges the motion maps of the last moments.
func unionCells(list []timedCells, now time.Time) []bool {
	out := make([]bool, motion.SearchW*motion.SearchH)
	for _, tc := range list {
		if now.Sub(tc.at) > cellMemory {
			continue
		}
		for i, v := range tc.cells {
			if v && i < len(out) {
				out[i] = true
			}
		}
	}
	return out
}

// candidates keeps boxes of wanted labels above their thresholds.
func candidates(boxes []ai.Box, cfg Config) []LiveObject {
	out := []LiveObject{}
	for _, b := range boxes {
		label := ai.Label(b.Class)
		score := float64(b.Score)
		if !cfg.liveAllow(label, score) {
			continue
		}
		if b.W*b.H < 0.0004 { // under 2% × 2% of the frame
			continue
		}
		out = append(out, LiveObject{Label: label, Score: round3(score), X: round3(float64(b.X)), Y: round3(float64(b.Y)), W: round3(float64(b.W)), H: round3(float64(b.H))})
	}
	return out
}

// acceptLocked decides which tracked candidates count (see Live). Candidates
// must already carry their track ids.
func acceptLocked(c *liveCam, cands []LiveObject, moved []bool, cfg Config, at time.Time) []LiveObject {
	out := []LiveObject{}
	for _, o := range cands {
		var t *tracked
		for i := range c.tracks {
			if c.tracks[i].id == o.ID {
				t = &c.tracks[i]
				break
			}
		}
		b := ai.Box{X: float32(o.X), Y: float32(o.Y), W: float32(o.W), H: float32(o.H)}
		person := o.Label == "person"
		if t != nil && overlapsMotion(b, moved) {
			switch {
			case person:
				t.movedAt = at
			case displaced(t.anchor, o):
				// It went somewhere itself, not just something in front of it.
				t.movedAt, t.anchor = at, o
			}
		}
		recentlyMoved := t != nil && !t.movedAt.IsZero() && at.Sub(t.movedAt) < stickyFor
		stillPerson := person && o.Score >= personStill(cfg)
		// Once boxed, a person stays boxed while still recognised at the normal
		// threshold: no blinking when their score wobbles. (Anything else keeps
		// its box for a minute after it last moved, then lets go.)
		wasShown := person && t != nil && !t.shownAt.IsZero() && at.Sub(t.shownAt) < stickyFor
		if recentlyMoved || stillPerson || wasShown {
			if t != nil {
				t.shownAt = at
			}
			out = append(out, o)
		}
	}
	// An object boxed a moment ago that the model missed in this frame keeps
	// its last box briefly, instead of blinking out.
	for _, t := range c.tracks {
		if t.seen.Equal(at) || t.shownAt.IsZero() || at.Sub(t.shownAt) > coastFor {
			continue
		}
		out = append(out, t.box)
	}
	return out
}

// parkedAfter is how long a vehicle must stand in view, not moving, to count
// as parked.
const parkedAfter = 3 * time.Second

// parkedLocked counts the vehicles in view that stand still: recognised in
// this frame for a few seconds now, but not boxed (they haven't moved within
// the last minute). The live view marks such a camera with a "P".
func parkedLocked(c *liveCam, shown []LiveObject, at time.Time) int {
	boxed := map[int64]bool{}
	for _, o := range shown {
		boxed[o.ID] = true
	}
	n := 0
	for _, t := range c.tracks {
		switch t.label {
		case "car", "truck", "bus", "motorcycle":
		default:
			continue
		}
		if t.seen.Equal(at) && !boxed[t.id] && at.Sub(t.born) >= parkedAfter {
			n++
		}
	}
	return n
}

// displaced reports whether an object's box went somewhere since a: its
// centre moved by a fifth of its size (at least 1.5% of the frame), or it grew
// or shrank by 30% (coming closer, going away). A parked car's box wobbles less
// than that from frame to frame.
func displaced(a, b LiveObject) bool {
	dx := (b.X + b.W/2) - (a.X + a.W/2)
	dy := (b.Y + b.H/2) - (a.Y + a.H/2)
	if math.Hypot(dx, dy) >= max(0.015, 0.2*min(a.W, a.H)) {
		return true
	}
	ra, rb := a.W*a.H, b.W*b.H
	return ra > 0 && (rb/ra > 1.3 || rb/ra < 1/1.3)
}

// personStill is the confidence a person needs to count without moving.
func personStill(cfg Config) float64 {
	if th, ok := cfg.Labels["person"]; ok && th > 0 {
		return th + stillBonus
	}
	return threshold("person", cfg.Sensitivity) + stillBonus
}

// watch counts a live viewer of a camera; the returned func uncounts it.
func (l *Live) watch(cameraID string) func() {
	l.mu.Lock()
	l.cam(cameraID).viewers++
	l.mu.Unlock()
	l.poke()
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			c := l.cam(cameraID)
			c.viewers--
			idle := c.viewers <= 0 && !c.moving
			if c.viewers < 0 {
				c.viewers = 0
			}
			shown := c.shown
			if idle {
				c.frame, c.cells, c.tracks, c.shown = nil, nil, nil, false
			}
			l.mu.Unlock()
			if idle && shown {
				l.publish("objects.live", cameraID, map[string]any{"ts": time.Now().UnixMilli(), "objects": []LiveObject{}})
			}
		})
	}
}

func overlapsMotion(b ai.Box, moved []bool) bool {
	gw, gh := motion.SearchW, motion.SearchH
	x0 := clampInt(int(b.X*float32(gw)), 0, gw-1)
	x1 := clampInt(int((b.X+b.W)*float32(gw)), 0, gw-1)
	y0 := clampInt(int(b.Y*float32(gh)), 0, gh-1)
	y1 := clampInt(int((b.Y+b.H)*float32(gh)), 0, gh-1)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if moved[y*gw+x] {
				return true
			}
		}
	}
	return false
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func round3(v float64) float64 { return float64(int(v*1000+0.5)) / 1000 }
