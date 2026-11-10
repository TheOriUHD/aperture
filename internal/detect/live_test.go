package detect

import (
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/ai"
	"github.com/TheOriUHD/aperture/internal/motion"
)

func cellsAt(x0, y0, x1, y1 float64) []bool {
	c := make([]bool, motion.SearchW*motion.SearchH)
	for y := int(y0 * motion.SearchH); y <= int(y1*motion.SearchH) && y < motion.SearchH; y++ {
		for x := int(x0 * motion.SearchW); x <= int(x1*motion.SearchW) && x < motion.SearchW; x++ {
			c[y*motion.SearchW+x] = true
		}
	}
	return c
}

func TestWhatCounts(t *testing.T) {
	cfg := Config{Mode: ModeLocal}
	person := 0
	// Someone walking on the left; a still "person" (a lamp post) on the right;
	// a TV nobody asked about; a person too unsure to report.
	boxes := []ai.Box{
		{Class: person, Score: 0.88, X: 0.10, Y: 0.30, W: 0.12, H: 0.50},
		{Class: person, Score: 0.70, X: 0.75, Y: 0.30, W: 0.10, H: 0.50},
		{Class: 62 /* tv */, Score: 0.9, X: 0.40, Y: 0.35, W: 0.05, H: 0.05},
		{Class: person, Score: 0.50, X: 0.45, Y: 0.30, W: 0.10, H: 0.50},
	}
	l := &Live{cams: map[string]*liveCam{}}
	c := l.cam("cam")
	t0 := time.Now()
	cands := candidates(boxes, cfg)
	if len(cands) != 2 {
		t.Fatalf("candidates = %+v", cands)
	}
	l.trackLocked(c, cands, t0)
	moved := cellsAt(0.12, 0.4, 0.2, 0.6)
	got := acceptLocked(c, cands, moved, cfg, t0)
	if len(got) != 1 || got[0].X != 0.1 {
		t.Fatalf("accepted %+v", got)
	}
	// The walker sits down: no motion any more, still boxed (moved recently).
	still := []bool{}
	still = append(still, make([]bool, len(moved))...)
	cands = candidates(boxes, cfg)
	l.trackLocked(c, cands, t0.Add(10*time.Second))
	if got := acceptLocked(c, cands, still, cfg, t0.Add(10*time.Second)); len(got) != 1 || got[0].X != 0.1 {
		t.Fatalf("sitting person lost: %+v", got)
	}
	// A clearly visible person who never moved counts; a 0.70 one does not.
	clear := candidates([]ai.Box{{Class: person, Score: 0.9, X: 0.5, Y: 0.1, W: 0.1, H: 0.5}}, cfg)
	c2 := l.cam("cam2")
	l.trackLocked(c2, clear, t0)
	if got := acceptLocked(c2, clear, still, cfg, t0); len(got) != 1 {
		t.Fatalf("confident still person dropped: %+v", got)
	}
	// High sensitivity lets the unsure one through as a candidate.
	cfg.Sensitivity = "high"
	if got := candidates(boxes, cfg); len(got) != 3 {
		t.Fatalf("high sensitivity candidates = %d", len(got))
	}
	// An explicit list without thresholds keeps only those labels.
	cfg = Config{Mode: ModeLocal, Labels: map[string]float64{"car": 0}}
	if got := candidates(boxes, cfg); len(got) != 0 {
		t.Fatalf("label list ignored: %+v", got)
	}
}

func TestEventCollectsBestPerLabel(t *testing.T) {
	l := &Live{cams: map[string]*liveCam{}}
	c := l.cam("cam")
	c.event = "ev1"
	c.firstFound = make(chan struct{})
	save, id := l.aggregateLocked(c, []LiveObject{{Label: "person", Score: 0.7}})
	if !save || id != "ev1" {
		t.Fatalf("first sighting should be saved, got %v %q", save, id)
	}
	select {
	case <-c.firstFound:
		t.Fatal("firstFound should have been closed and cleared")
	default:
	}
	if c.firstFound != nil {
		t.Fatal("firstFound not cleared")
	}
	// A better score within the save interval is kept but not saved yet.
	if save, _ := l.aggregateLocked(c, []LiveObject{{Label: "person", Score: 0.92}}); save {
		t.Fatal("saved again inside the interval")
	}
	if c.best["person"].Confidence != 0.92 {
		t.Fatalf("best = %v", c.best["person"].Confidence)
	}
	c.savedAt = time.Now().Add(-3 * time.Second)
	if save, _ := l.aggregateLocked(c, []LiveObject{{Label: "dog", Score: 0.8}}); !save {
		t.Fatal("a new label after the interval should be saved")
	}
	if len(bestList(c.best)) != 2 || bestList(c.best)[0].Label != "person" {
		t.Fatalf("best list = %+v", bestList(c.best))
	}
}

func TestTrackerKeepsIDs(t *testing.T) {
	l := &Live{cams: map[string]*liveCam{}}
	c := l.cam("cam")
	t0 := time.Now()
	// Two people side by side, walking right.
	f1 := []LiveObject{{Label: "person", Score: 0.9, X: 0.1, Y: 0.2, W: 0.1, H: 0.5}, {Label: "person", Score: 0.8, X: 0.6, Y: 0.2, W: 0.1, H: 0.5}}
	l.trackLocked(c, f1, t0)
	a, b := f1[0].ID, f1[1].ID
	if a == 0 || b == 0 || a == b {
		t.Fatalf("ids = %d, %d", a, b)
	}
	// A third of a second later each moved a little (no overlap for the fast one).
	f2 := []LiveObject{{Label: "person", Score: 0.85, X: 0.72, Y: 0.2, W: 0.1, H: 0.5}, {Label: "person", Score: 0.9, X: 0.13, Y: 0.2, W: 0.1, H: 0.5}}
	l.trackLocked(c, f2, t0.Add(300*time.Millisecond))
	if f2[1].ID != a || f2[0].ID != b {
		t.Fatalf("ids swapped or lost: %d %d (want %d %d)", f2[1].ID, f2[0].ID, a, b)
	}
	// A dog is a new object, not one of the people.
	f3 := []LiveObject{{Label: "dog", Score: 0.9, X: 0.13, Y: 0.6, W: 0.1, H: 0.1}}
	l.trackLocked(c, f3, t0.Add(600*time.Millisecond))
	if f3[0].ID == a || f3[0].ID == b {
		t.Fatal("dog took a person's id")
	}
	// Seen again after the track was forgotten: a new id.
	f4 := []LiveObject{{Label: "person", Score: 0.9, X: 0.13, Y: 0.2, W: 0.1, H: 0.5}}
	l.trackLocked(c, f4, t0.Add(5*time.Second))
	if f4[0].ID == a {
		t.Fatal("kept an id across a long gap")
	}
}

func TestBoxedObjectsDontBlink(t *testing.T) {
	cfg := Config{Mode: ModeLocal}
	l := &Live{cams: map[string]*liveCam{}}
	c := l.cam("cam")
	still := make([]bool, motion.SearchW*motion.SearchH)
	t0 := time.Now()
	frame := func(score float32, dt time.Duration) []LiveObject {
		var boxes []ai.Box
		if score > 0 {
			boxes = []ai.Box{{Class: 0, Score: score, X: 0.6, Y: 0.5, W: 0.12, H: 0.45}}
		}
		cands := candidates(boxes, cfg)
		l.trackLocked(c, cands, t0.Add(dt))
		return acceptLocked(c, cands, still, cfg, t0.Add(dt))
	}
	if got := frame(0.80, 0); len(got) != 1 {
		t.Fatalf("confident still person not boxed: %+v", got)
	}
	// Score wobbles under the still-person bar but above the normal one: stays.
	if got := frame(0.70, 700*time.Millisecond); len(got) != 1 {
		t.Fatal("box blinked out on a lower score")
	}
	// One frame where the model misses: the box coasts.
	if got := frame(0, 1400*time.Millisecond); len(got) != 1 {
		t.Fatal("box blinked out on a missed frame")
	}
	// Gone for good: the box goes.
	if got := frame(0, 3500*time.Millisecond); len(got) != 0 {
		t.Fatalf("box outlived its object: %+v", got)
	}
}

// A parked car stays unboxed however much moves around it; a car that drives
// in is boxed, and lets go a minute after it parks.
func TestParkedCarsDontCount(t *testing.T) {
	cfg := Config{Mode: ModeLocal}
	l := &Live{cams: map[string]*liveCam{}}
	c := l.cam("cam")
	t0 := time.Now()
	car := 2 // COCO car
	look := func(dt time.Duration, x, w float32, moved []bool) []LiveObject {
		cands := candidates([]ai.Box{{Class: car, Score: 0.9, X: x, Y: 0.2, W: w, H: 0.3}}, cfg)
		l.trackLocked(c, cands, t0.Add(dt))
		return acceptLocked(c, cands, moved, cfg, t0.Add(dt))
	}
	everywhere := cellsAt(0, 0, 1, 1)
	// Parked, with someone walking past and headlights over it; its box
	// wobbles a little from frame to frame.
	for i := 0; i < 20; i++ {
		jitter := float32(i%3) * 0.004
		if got := look(time.Duration(i)*200*time.Millisecond, 0.3+jitter, 0.4-jitter, everywhere); len(got) != 0 {
			t.Fatalf("parked car boxed at frame %d: %+v", i, got)
		}
	}
	// Standing there for 4 s: it counts as parked (the "P" in the live view).
	if n := parkedLocked(c, nil, t0.Add(19*200*time.Millisecond)); n != 1 {
		t.Fatalf("parked = %d, want 1", n)
	}
	// It drives off to the right: boxed.
	var got []LiveObject
	for i := 0; i < 5; i++ {
		got = look(4*time.Second+time.Duration(i)*200*time.Millisecond, 0.3+float32(i)*0.03, 0.4, everywhere)
	}
	if len(got) != 1 {
		t.Fatalf("driving car not boxed: %+v", got)
	}
	if n := parkedLocked(c, got, t0.Add(4*time.Second+4*200*time.Millisecond)); n != 0 {
		t.Fatalf("a driving car counted as parked")
	}
	// It stops there: still boxed for a while, then let go.
	for dt := 5 * time.Second; dt <= 30*time.Second; dt += time.Second {
		got = look(dt, 0.42, 0.4, everywhere)
	}
	if len(got) != 1 {
		t.Fatalf("car lost its box right after stopping: %+v", got)
	}
	for dt := 31 * time.Second; dt < 70*time.Second; dt += time.Second {
		look(dt, 0.42, 0.4, everywhere)
	}
	if got := look(70*time.Second, 0.42, 0.4, everywhere); len(got) != 0 {
		t.Fatalf("parked car still boxed a minute after it stopped: %+v", got)
	}
}

// Presence counts what recognition counted, and holds a few seconds past the
// last frame that saw it (no flapping on a missed frame).
func TestPresence(t *testing.T) {
	cfg := Config{Mode: ModeLocal}
	l := &Live{cams: map[string]*liveCam{}}
	c := l.cam("room")
	c.enabled = true
	t0 := time.Now()
	two := candidates([]ai.Box{
		{Class: 0, Score: 0.9, X: 0.1, Y: 0.2, W: 0.15, H: 0.6},
		{Class: 0, Score: 0.88, X: 0.6, Y: 0.2, W: 0.15, H: 0.6},
	}, cfg)
	l.trackLocked(c, two, t0)
	objs := acceptLocked(c, two, make([]bool, len(cellsAt(0, 0, 0, 0))), cfg, t0)
	c.notePresenceLocked(objs, 1, t0)
	c.parked, c.parkedAt = 1, t0

	p, ok := l.presence("room", t0.Add(time.Second))
	if !ok || !p.Person || p.People != 2 || !p.Vehicle || !p.Parked || p.Animal {
		t.Fatalf("presence = %+v (ok %v)", p, ok)
	}
	if p, _ := l.presence("room", t0.Add(presenceHold+time.Second)); p.Person || p.People != 0 || p.Parked {
		t.Fatalf("presence outlived its hold: %+v", p)
	}
	if _, ok := l.presence("garden", t0); ok {
		t.Fatal("a camera recognition doesn't look at reported presence")
	}
}
