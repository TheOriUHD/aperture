package detect

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/TheOriUHD/aperture/internal/ai"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Package watch. On the cameras chosen for it, the package model looks at the
// scene now and then: a few seconds after motion settles, every few seconds
// while something is pending, and every half minute otherwise. It keeps a list
// of the parcel-like things standing there:
//
//   - something new that stays put (seen twice, a while apart, with nobody
//     holding it) was delivered, if the camera saw motion before it appeared;
//   - something delivered that's gone for good (missed twice, with nobody in
//     front of it) was picked up, if the camera saw motion since.
//
// The motion rule carries most of the weight: a real flat box seen from above
// at night scored only 0.2-0.4, so the threshold is low, and a parcel can't
// come or go without someone moving. Something that turns up when nothing has
// moved for a while is the model wavering on the scene (a doormat at dusk), and
// becomes part of the scene.
//
// Each is an event of type "package", so notifications, webhooks and
// automations see it. Whatever stands there when watching begins (a storage
// box, a bin the model takes for a parcel) is the scene, not a delivery: it's
// known from the first look on, without an event.
//
// Recognising a courier by their looks (uniform, logo) isn't something the
// open models can do; the delivery is told by what happens instead.

const (
	pkgIdleEvery    = 30 * time.Second // look this often when all is quiet
	pkgBusyEvery    = 8 * time.Second  // while moving, or while something is pending
	pkgSettle       = 3 * time.Second  // after motion stops, before looking
	pkgConfirmAfter = 8 * time.Second  // a new parcel must stand this long
	pkgGoneAfter    = 15 * time.Second // a missing parcel must be gone this long
	pkgKeepMargin   = 0.1              // a known parcel stays known down to this much below the threshold
	pkgMotionWindow = 2 * time.Minute  // a new parcel counts if something moved this recently
	pkgMinArea      = 0.0004           // smaller boxes are noise
	pkgMaxArea      = 0.4              // bigger ones aren't a parcel
)

// packageThreshold is the score a new parcel needs.
func packageThreshold(sensitivity string) float64 {
	switch sensitivity {
	case "low":
		return 0.35
	case "high":
		return 0.18
	}
	return 0.25
}

type packages struct {
	e  *Engine
	ai *ai.Manager

	mu     sync.Mutex
	cams   map[string]*pkgCam
	busy   bool // a check is running (one at a time: the worker is shared)
	nextID int64
}

type pkgCam struct {
	on        bool
	baselined bool // the first look happened
	checking  bool
	next      time.Time
	checkedAt time.Time // the last look
	observed  time.Time // the last live frame taken as a look
	lastLook  time.Time // the look before this one (in update)
	moving    bool
	motionAt  time.Time // when motion last started or stopped
	best      float32   // the best package score of the last look
	parcels   []*parcel
}

// moved reports whether the camera saw motion since t.
func (c *pkgCam) moved(since time.Time) bool {
	return c.moving || c.motionAt.After(since)
}

// parcel is one parcel-like thing standing in view.
type parcel struct {
	id        int64
	box       ai.Box
	firstSeen time.Time
	lastSeen  time.Time
	hits      int
	misses    int
	missing   time.Time // the first look that missed it
	scene     bool      // there from the first look: part of the scene
	confirmed bool
}

func newPackages(e *Engine, m *ai.Manager) *packages {
	return &packages{e: e, ai: m, cams: map[string]*pkgCam{}}
}

// setCameras records which cameras watch for packages.
func (p *packages) setCameras(on map[string]bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, c := range p.cams {
		if !on[id] {
			c.on = false
			c.parcels = nil
			c.baselined = false
		}
	}
	for id := range on {
		c := p.cams[id]
		if c == nil {
			c = &pkgCam{}
			p.cams[id] = c
		}
		if !c.on {
			c.on = true
			c.next = time.Now().Add(2 * time.Second)
		}
	}
}

// offer is handed every colour frame of a camera; it starts a check when one
// is due.
func (p *packages) offer(id string, w, h int, rgb []byte) {
	now := time.Now()
	p.mu.Lock()
	c := p.cams[id]
	if c == nil || !c.on || c.checking || p.busy || now.Before(c.next) {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	if !p.ai.PackagesReady() {
		return
	}
	p.mu.Lock()
	if c.checking || p.busy {
		p.mu.Unlock()
		return
	}
	c.checking, p.busy = true, true
	frame := make([]byte, len(rgb))
	copy(frame, rgb)
	p.mu.Unlock()
	go p.check(id, frame, w, h)
}

// motion tells the watch a camera started or stopped moving.
func (p *packages) motion(id string, active bool) {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.cams[id]
	if c == nil || !c.on {
		return
	}
	c.moving = active
	c.motionAt = now
	soon := now.Add(pkgBusyEvery)
	if !active {
		soon = now.Add(pkgSettle)
	}
	if soon.Before(c.next) {
		c.next = soon
	}
}

// watching reports whether a camera watches for packages.
func (p *packages) watching(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.cams[id]
	return c != nil && c.on
}

// liveCandidates are the parcels to box live in a frame of the all-in-one
// model: standing ones above the threshold, not in anyone's hands, and not
// part of the scene (what stood there all along).
func (p *packages) liveCandidates(id string, boxes []ai.Box, sensitivity string) []LiveObject {
	th := packageThreshold(sensitivity)
	var people []ai.Box
	for _, b := range boxes {
		if b.Class == 0 && b.Score >= 0.4 {
			people = append(people, b)
		}
	}
	p.mu.Lock()
	var scene []ai.Box
	if c := p.cams[id]; c != nil {
		for _, pc := range c.parcels {
			if pc.scene {
				scene = append(scene, pc.box)
			}
		}
	}
	p.mu.Unlock()
	var out []LiveObject
	for _, b := range boxes {
		if b.Class != ai.PackageClass || float64(b.Score) < th {
			continue
		}
		area := float64(b.W * b.H)
		if area < pkgMinArea || area > pkgMaxArea || covered(b, people, 0.5) {
			continue
		}
		inScene := false
		for _, s := range scene {
			if ai.IoU(s, b) > 0.3 {
				inScene = true
				break
			}
		}
		if !inScene {
			out = append(out, LiveObject{Label: "package", Score: round2(float64(b.Score)),
				X: round3(float64(b.X)), Y: round3(float64(b.Y)), W: round3(float64(b.W)), H: round3(float64(b.H))})
		}
	}
	return out
}

// observe takes a live frame of the all-in-one model as a look (at most one a
// second): parcels are noticed as they happen, not at the next scheduled look.
func (p *packages) observe(id string, boxes []ai.Box, frame []byte, w, h int) {
	now := time.Now()
	th := packageThreshold(p.e.LoadConfig().Sensitivity)
	p.mu.Lock()
	c := p.cams[id]
	if c == nil || !c.on || c.checking || now.Sub(c.observed) < time.Second {
		p.mu.Unlock()
		return
	}
	c.observed, c.checkedAt = now, now
	delivered, taken := c.update(ai.PeopleAndPackages(boxes), th, now, func() int64 { p.nextID++; return p.nextID })
	// Live frames keep coming while it moves or is watched: the scheduled
	// look can wait.
	if later := now.Add(pkgBusyEvery); c.next.Before(later) {
		c.next = later
	}
	p.mu.Unlock()
	if len(delivered)+len(taken) == 0 {
		return
	}
	cp := make([]byte, len(frame))
	copy(cp, frame)
	for _, pc := range delivered {
		p.raise(id, pc, true, cp, w, h)
	}
	for _, pc := range taken {
		p.raise(id, pc, false, cp, w, h)
	}
}

// liveObjects are the delivered parcels to draw in the live view.
func (p *packages) liveObjects(id string) []LiveObject {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.cams[id]
	if c == nil {
		return nil
	}
	var out []LiveObject
	for _, pc := range c.parcels {
		if pc.confirmed && !pc.scene {
			b := pc.box
			out = append(out, LiveObject{ID: -pc.id, Label: "package", Score: round2(float64(b.Score)),
				X: float64(b.X), Y: float64(b.Y), W: float64(b.W), H: float64(b.H)})
		}
	}
	return out
}

func (p *packages) check(id string, frame []byte, w, h int) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	res, err := p.ai.DetectPackages(ctx, frame, w, h)
	cancel()
	th := packageThreshold(p.e.LoadConfig().Sensitivity)
	now := time.Now()

	p.mu.Lock()
	p.busy = false
	c := p.cams[id]
	if c == nil {
		p.mu.Unlock()
		return
	}
	c.checking = false
	if err != nil || !c.on {
		c.next = now.Add(15 * time.Second)
		p.mu.Unlock()
		return
	}
	c.checkedAt = now
	delivered, taken := c.update(res.Boxes, th, now, func() int64 { p.nextID++; return p.nextID })
	switch {
	case c.moving || c.pending():
		c.next = now.Add(pkgBusyEvery)
	default:
		c.next = now.Add(pkgIdleEvery)
	}
	p.mu.Unlock()

	for _, pc := range delivered {
		p.raise(id, pc, true, frame, w, h)
	}
	for _, pc := range taken {
		p.raise(id, pc, false, frame, w, h)
	}
}

// pending reports whether something waits to be confirmed or to be gone.
func (c *pkgCam) pending() bool {
	for _, pc := range c.parcels {
		if !pc.confirmed || pc.misses > 0 {
			return true
		}
	}
	return false
}

// update takes one look's boxes (people and packages) and returns the parcels
// just delivered and just picked up.
func (c *pkgCam) update(boxes []ai.Box, th float64, now time.Time, newID func() int64) (delivered, taken []parcel) {
	c.lastLook = now
	var people, cands []ai.Box
	c.best = 0
	for _, b := range boxes {
		switch b.Class {
		case 0:
			if b.Score >= 0.4 {
				people = append(people, b)
			}
		case ai.PackageClass:
			c.best = max(c.best, b.Score)
			area := float64(b.W * b.H)
			if float64(b.Score) >= th-pkgKeepMargin && area >= pkgMinArea && area <= pkgMaxArea && !covered(b, people, 0.5) {
				cands = append(cands, b)
			}
		}
	}

	// Known parcels first: each takes the candidate overlapping it most.
	used := make([]bool, len(cands))
	keep := c.parcels[:0]
	for _, pc := range c.parcels {
		best, bestIoU := -1, float32(0.3)
		for i, b := range cands {
			if !used[i] {
				if v := ai.IoU(pc.box, b); v > bestIoU {
					best, bestIoU = i, v
				}
			}
		}
		if best >= 0 {
			used[best] = true
			pc.box, pc.lastSeen, pc.misses = cands[best], now, 0
			pc.hits++
		} else if !covered(pc.box, people, 0.3) && (!pc.confirmed || pc.scene || c.moved(pc.lastSeen)) {
			// Not just someone standing in front of it, and (for a delivered
			// parcel) someone was there to take it: missing.
			if pc.misses == 0 {
				pc.missing = now
			}
			pc.misses++
		}
		if pc.misses >= 2 && now.Sub(pc.missing) >= pkgGoneAfter {
			if pc.confirmed && !pc.scene {
				taken = append(taken, *pc)
			}
			continue
		}
		if !pc.confirmed && pc.hits >= 2 && now.Sub(pc.firstSeen) >= pkgConfirmAfter {
			pc.confirmed = true
			delivered = append(delivered, *pc)
		}
		keep = append(keep, pc)
	}
	c.parcels = keep

	// New ones need the full threshold.
	for i, b := range cands {
		if used[i] || float64(b.Score) < th {
			continue
		}
		pc := &parcel{id: newID(), box: b, firstSeen: now, lastSeen: now, hits: 1}
		if !c.baselined || !c.moved(now.Add(-pkgMotionWindow)) {
			// There from the start, or turned up with nothing moving.
			pc.scene, pc.confirmed = true, true
		}
		c.parcels = append(c.parcels, pc)
	}
	c.baselined = true
	return delivered, taken
}

// covered reports whether people cover at least frac of box b.
func covered(b ai.Box, people []ai.Box, frac float32) bool {
	area := b.W * b.H
	if area <= 0 {
		return false
	}
	for _, p := range people {
		x0, y0 := max(b.X, p.X), max(b.Y, p.Y)
		x1, y1 := min(b.X+b.W, p.X+p.W), min(b.Y+b.H, p.Y+p.H)
		if x1 > x0 && y1 > y0 && (x1-x0)*(y1-y0)/area >= frac {
			return true
		}
	}
	return false
}

// raise records a delivery or a pick-up as an event, with the frame as its
// picture, and tells notifications, webhooks and automations.
func (p *packages) raise(cameraID string, pc parcel, delivered bool, frame []byte, w, h int) {
	now := time.Now()
	summary, start := "package delivered", pc.firstSeen
	if !delivered {
		summary, start = "package picked up", pc.lastSeen
	}
	b := pc.box
	ev := &store.Event{ID: store.NewID(), CameraID: cameraID, Type: "package", StartTS: start.UnixMilli(),
		EndTS: now.UnixMilli(), Score: float64(b.Score)}
	if err := p.e.st.InsertEvent(ev); err != nil {
		p.e.log.Error("packages: insert event", "err", err)
		return
	}
	obj := store.EventObject{Label: "package", Confidence: round2(float64(b.Score)),
		X: float64(b.X), Y: float64(b.Y), W: float64(b.W), H: float64(b.H), TS: now.UnixMilli()}
	if err := p.e.st.SaveEventDetections(ev.ID, "package", summary, []store.EventObject{obj}); err != nil {
		p.e.log.Error("packages: save event objects", "err", err)
	}
	if p.e.eventDir != "" {
		if path, err := writeJPEG(filepath.Join(p.e.eventDir, cameraID, "events"), ev.ID, frame, w, h); err == nil {
			_ = p.e.st.SetEventThumb(ev.ID, path)
		}
	}
	ev.TopLabel, ev.ObjectSummary = "package", summary
	ev.Labels = []string{"package"}
	ev.Objects = []store.EventObject{obj}
	p.e.log.Info("packages: "+summary, "camera", cameraID, "score", round2(float64(b.Score)))
	if p.e.onDetections != nil {
		go p.e.onDetections(ev.ID, cameraID)
	}
	if p.e.onEvent != nil {
		go p.e.onEvent(ev)
	}
}

// writeJPEG saves a packed-RGB frame as dir/name.jpg.
func writeJPEG(dir, name string, rgb []byte, w, h int) (string, error) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		copy(img.Pix[i*4:i*4+3], rgb[i*3:i*3+3])
		img.Pix[i*4+3] = 255
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+".jpg")
	return path, os.WriteFile(path, buf.Bytes(), 0o644)
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// PackageWatch is what a camera's package watch knows, for the settings page.
type PackageWatch struct {
	CheckedAt int64   `json:"checkedAt,omitempty"` // unix ms of the last look
	Parcels   int     `json:"parcels"`             // delivered parcels standing there now
	Scene     int     `json:"scene"`               // parcel-like things that were there from the start
	Best      float64 `json:"best"`                // best package score of the last look (diagnostics)
}

// PackageWatches reports the package watch of each camera that has one.
func (e *Engine) PackageWatches() map[string]PackageWatch {
	out := map[string]PackageWatch{}
	if e.pkgs == nil {
		return out
	}
	p := e.pkgs
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, c := range p.cams {
		if !c.on {
			continue
		}
		w := PackageWatch{Best: round2(float64(c.best))}
		if !c.checkedAt.IsZero() {
			w.CheckedAt = c.checkedAt.UnixMilli()
		}
		for _, pc := range c.parcels {
			switch {
			case pc.scene:
				w.Scene++
			case pc.confirmed:
				w.Parcels++
			}
		}
		out[id] = w
	}
	return out
}
