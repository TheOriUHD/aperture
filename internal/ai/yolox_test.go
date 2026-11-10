package ai

import (
	"math"
	"testing"
)

func TestLetterboxPadsAndKeepsAspect(t *testing.T) {
	// A 4×2 red frame into an 8×8 input: scaled ×2 to 8×4, grey below.
	w, h, size := 4, 2, 8
	rgb := make([]byte, w*h*3)
	for i := 0; i < w*h; i++ {
		rgb[i*3] = 255 // red
	}
	dst := make([]float32, 3*size*size)
	scale := Letterbox(rgb, w, h, size, dst)
	if scale != 2 {
		t.Fatalf("scale = %v, want 2", scale)
	}
	plane := size * size
	// Inside the image: B=0, G=0, R=255 (planes are BGR).
	if dst[0*plane+0] != 0 || dst[1*plane+0] != 0 || dst[2*plane+0] != 255 {
		t.Fatalf("top-left pixel = B%v G%v R%v, want B0 G0 R255", dst[0], dst[plane], dst[2*plane])
	}
	// Row 5 is padding.
	if dst[0*plane+5*size] != 114 {
		t.Fatalf("padding = %v, want 114", dst[5*size])
	}
}

// fakeOutput builds a YOLOX-style output with one confident cell.
func fakeOutput(size int, classes int, stride, gx, gy int, dx, dy, lw, lh, obj, cls float32, class int) ([]float32, []int64) {
	rows := 0
	for _, s := range []int{8, 16, 32} {
		rows += (size / s) * (size / s)
	}
	cols := 5 + classes
	out := make([]float32, rows*cols)
	idx := 0
	for _, s := range []int{8, 16, 32} {
		g := size / s
		if s == stride {
			idx += gy*g + gx
			break
		}
		idx += g * g
	}
	p := out[idx*cols : (idx+1)*cols]
	p[0], p[1], p[2], p[3], p[4] = dx, dy, lw, lh, obj
	p[5+class] = cls
	return out, []int64{1, int64(rows), int64(cols)}
}

func TestDecodeYOLOX(t *testing.T) {
	size := 64
	// Cell (2,3) at stride 8, centred, 16×32 px box: centre (20, 28).
	out, dims := fakeOutput(size, 80, 8, 2, 3, 0.5, 0.5, float32(math.Log(2)), float32(math.Log(4)), 0.9, 0.9, 0)
	// Source frame 128×128 letterboxed at scale 0.5.
	boxes := DecodeYOLOX(out, dims, size, 0.5, 128, 128, 0.3)
	if len(boxes) != 1 {
		t.Fatalf("got %d boxes, want 1", len(boxes))
	}
	b := boxes[0]
	if Label(b.Class) != "person" {
		t.Fatalf("label = %s", Label(b.Class))
	}
	// Model box (12..28, 12..44) → source ×2 → (24..56, 24..88) of 128.
	want := Box{X: 24.0 / 128, Y: 24.0 / 128, W: 32.0 / 128, H: 64.0 / 128}
	for _, c := range [][2]float32{{b.X, want.X}, {b.Y, want.Y}, {b.W, want.W}, {b.H, want.H}} {
		if math.Abs(float64(c[0]-c[1])) > 1e-4 {
			t.Fatalf("box = %+v, want %+v", b, want)
		}
	}
	if math.Abs(float64(b.Score-0.81)) > 1e-4 {
		t.Fatalf("score = %v, want 0.81", b.Score)
	}
	if got := DecodeYOLOX(out, dims, size, 0.5, 128, 128, 0.85); len(got) != 0 {
		t.Fatalf("threshold 0.85 kept %d boxes", len(got))
	}
}

func TestNMSMergesOverlaps(t *testing.T) {
	boxes := []Box{
		{Class: 0, Score: 0.6, X: 0.1, Y: 0.1, W: 0.2, H: 0.4},
		{Class: 0, Score: 0.9, X: 0.11, Y: 0.1, W: 0.2, H: 0.4},
		{Class: 2, Score: 0.7, X: 0.11, Y: 0.1, W: 0.2, H: 0.4}, // another class survives
		{Class: 0, Score: 0.5, X: 0.7, Y: 0.1, W: 0.2, H: 0.4},  // far away survives
	}
	got := NMS(boxes, 0.45)
	if len(got) != 3 || got[0].Score != 0.9 {
		t.Fatalf("NMS = %+v", got)
	}
}

func TestTilesForPanoramicFrames(t *testing.T) {
	if got := Tiles(416, 234); len(got) != 1 || got[0] != [2]int{0, 416} {
		t.Fatalf("16:9 frame tiled: %v", got)
	}
	got := Tiles(1248, 352) // 3.55:1, like a 1536×432 camera
	if len(got) != 3 {
		t.Fatalf("tiles = %v", got)
	}
	if got[0][0] != 0 || got[2][0]+got[2][1] != 1248 {
		t.Fatalf("tiles don't cover the frame: %v", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i][0] >= got[i-1][0]+got[i-1][1] {
			t.Fatalf("tiles %d and %d don't overlap: %v", i-1, i, got)
		}
	}
}

func TestDecodeDFINE(t *testing.T) {
	const q, c = 3, 366
	logits := make([]float32, q*c)
	for i := range logits {
		logits[i] = -9 // sigmoid ≈ 0
	}
	boxes := []float32{
		0.5, 0.5, 0.2, 0.4, // query 0: a person
		0.3, 0.8, 0.1, 0.1, // query 1: a parcel (luggage-like)
		0.9, 0.9, 0.1, 0.1, // query 2: nothing
	}
	logits[0*c+1] = 2     // person 0.88
	logits[1*c+121] = 0.5 // luggage 0.62
	logits[1*c+21] = 0.2  // storage box 0.55
	got := DecodeDFINE(logits, boxes, q, c, 0.3)
	if len(got) != 2 {
		t.Fatalf("got %d boxes: %+v", len(got), got)
	}
	for _, b := range got {
		switch b.Class {
		case 0:
			if b.X < 0.39 || b.X > 0.41 || b.H < 0.39 {
				t.Errorf("person box %+v", b)
			}
		case PackageClass:
			if b.Score < 0.61 || b.Score > 0.63 || b.X < 0.24 || b.X > 0.26 || Label(b.Class) != "package" {
				t.Errorf("package box %+v", b)
			}
		default:
			t.Errorf("class %d", b.Class)
		}
	}
}
