package detect

import (
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/ai"
)

func pkgBox(x, y float32, score float32) ai.Box {
	return ai.Box{Class: ai.PackageClass, Score: score, X: x, Y: y, W: 0.1, H: 0.1}
}

func personBox(x, y float32) ai.Box {
	return ai.Box{Class: 0, Score: 0.9, X: x, Y: y, W: 0.2, H: 0.6}
}

type pkgTest struct {
	c  *pkgCam
	n  int64
	t0 time.Time
}

func newPkgTest() *pkgTest { return &pkgTest{c: &pkgCam{on: true}, t0: time.Unix(1_000_000, 0)} }

func (p *pkgTest) at(s int) time.Time { return p.t0.Add(time.Duration(s) * time.Second) }

// move: the camera saw motion at second s.
func (p *pkgTest) move(s int) { p.c.motionAt = p.at(s) }

func (p *pkgTest) look(s int, boxes ...ai.Box) (int, int) {
	d, g := p.c.update(boxes, 0.3, p.at(s), func() int64 { p.n++; return p.n })
	return len(d), len(g)
}

func TestPackageDeliveredAndPickedUp(t *testing.T) {
	p := newPkgTest()
	// The first look: a storage box by the wall is part of the scene.
	if d, g := p.look(0, pkgBox(0.8, 0.8, 0.7)); d+g != 0 {
		t.Fatal("the scene raised an event")
	}
	// The courier carries a parcel in: held, it's not a delivery yet.
	p.move(5)
	if d, _ := p.look(8, pkgBox(0.8, 0.8, 0.7), personBox(0.4, 0.2), ai.Box{Class: ai.PackageClass, Score: 0.8, X: 0.45, Y: 0.4, W: 0.1, H: 0.1}); d != 0 {
		t.Fatal("a parcel in someone's hands counted")
	}
	// Set down by the door, the courier still next to it.
	p.move(13)
	if d, _ := p.look(16, pkgBox(0.8, 0.8, 0.7), personBox(0.6, 0.2), pkgBox(0.4, 0.7, 0.42)); d != 0 {
		t.Fatal("delivered on first sight")
	}
	// Still there after they left: delivered, once (a real box scored 0.41).
	p.move(20)
	if d, _ := p.look(24, pkgBox(0.8, 0.8, 0.7), pkgBox(0.41, 0.7, 0.35)); d != 1 {
		t.Fatalf("delivered = %d, want 1", d)
	}
	// Its score dips below the threshold: still known, no new delivery.
	if d, g := p.look(54, pkgBox(0.8, 0.8, 0.7), pkgBox(0.4, 0.7, 0.22)); d+g != 0 {
		t.Fatalf("a dip raised events (%d, %d)", d, g)
	}
	// The model loses it for a while with nothing moving: not taken.
	p.look(62, pkgBox(0.8, 0.8, 0.7))
	p.look(70, pkgBox(0.8, 0.8, 0.7))
	if d, g := p.look(90, pkgBox(0.8, 0.8, 0.7)); d+g != 0 || len(p.c.parcels) != 2 {
		t.Fatalf("lost from view without motion: events (%d, %d), %d parcels", d, g, len(p.c.parcels))
	}
	p.look(98, pkgBox(0.8, 0.8, 0.7), pkgBox(0.4, 0.7, 0.3))
	// Someone stands in front of it: not missing.
	p.move(100)
	p.look(104, pkgBox(0.8, 0.8, 0.7), personBox(0.35, 0.4))
	p.look(112, pkgBox(0.8, 0.8, 0.7), personBox(0.35, 0.4))
	if p.c.parcels[1].misses != 0 {
		t.Fatalf("hidden parcel counted as missing: %+v", p.c.parcels[1])
	}
	// Taken: missed twice, gone 15 s.
	if _, g := p.look(120, pkgBox(0.8, 0.8, 0.7)); g != 0 {
		t.Fatal("picked up after one miss")
	}
	if _, g := p.look(128, pkgBox(0.8, 0.8, 0.7)); g != 0 {
		t.Fatal("picked up before it was gone long enough")
	}
	if _, g := p.look(136, pkgBox(0.8, 0.8, 0.7)); g != 1 {
		t.Fatalf("picked up = %d, want 1", g)
	}
	// The scene box leaving says nothing.
	p.look(144)
	p.look(152)
	if d, g := p.look(170); d+g != 0 || len(p.c.parcels) != 0 {
		t.Fatalf("scene box raised events (%d, %d), left %d", d, g, len(p.c.parcels))
	}
}

// Something that turns up with nothing moving is the model wavering on the
// scene, not a delivery.
func TestPackageWithoutMotionIsScene(t *testing.T) {
	p := newPkgTest()
	p.look(0)
	for s := 30; s <= 120; s += 30 {
		if d, g := p.look(s, pkgBox(0.5, 0.8, 0.4)); d+g != 0 {
			t.Fatalf("a box that appeared without motion raised an event at %d s", s)
		}
	}
	if len(p.c.parcels) != 1 || !p.c.parcels[0].scene {
		t.Fatalf("parcels = %+v, want one scene parcel", p.c.parcels)
	}
}

func TestPackageFlickerIsNoDelivery(t *testing.T) {
	p := newPkgTest()
	p.look(0)
	// Seen once after motion, then never again: nothing.
	p.move(5)
	p.look(8, pkgBox(0.2, 0.2, 0.6))
	for s := 16; s < 70; s += 8 {
		if d, g := p.look(s); d+g != 0 {
			t.Fatal("a one-off sighting raised an event")
		}
	}
	if len(p.c.parcels) != 0 {
		t.Fatal("the one-off sighting was kept")
	}
}

// With an all-in-one model, a parcel put down is boxed in the very frame; one
// in someone's hands, or one that's part of the scene, is not.
func TestLiveParcelBoxes(t *testing.T) {
	e := &Engine{}
	p := newPackages(e, nil)
	p.cams["door"] = &pkgCam{on: true, baselined: true, parcels: []*parcel{
		{id: 1, box: pkgBox(0.8, 0.8, 0.6), scene: true, confirmed: true},
	}}
	frame := []ai.Box{
		pkgBox(0.8, 0.8, 0.6),  // the scene's storage box
		pkgBox(0.3, 0.7, 0.35), // just put down
		personBox(0.5, 0.2),
		{Class: ai.PackageClass, Score: 0.9, X: 0.55, Y: 0.4, W: 0.08, H: 0.08}, // carried
		pkgBox(0.1, 0.1, 0.2), // too unsure
	}
	got := p.liveCandidates("door", frame, "normal")
	if len(got) != 1 || got[0].X != 0.3 {
		t.Fatalf("live parcels = %+v, want only the one just put down", got)
	}
	// Boxed although it never moves.
	l := &Live{cams: map[string]*liveCam{}}
	c := l.cam("door")
	at := time.Now()
	l.trackLocked(c, got, at)
	if out := acceptLocked(c, got, make([]bool, len(cellsAt(0, 0, 0, 0))), Config{Mode: ModeLocal}, at); len(out) != 1 {
		t.Fatalf("standing parcel not boxed: %+v", out)
	}
}
