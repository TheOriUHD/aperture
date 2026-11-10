package ai

import (
	"math"
	"sort"
)

// Box is one detected object. Coordinates are normalised to the source frame
// (0..1, origin top left).
type Box struct {
	Class int     `json:"c"`
	Score float32 `json:"s"`
	X     float32 `json:"x"`
	Y     float32 `json:"y"`
	W     float32 `json:"w"`
	H     float32 `json:"h"`
}

// Letterbox converts a packed RGB24 frame (w×h) into the planar BGR float input
// YOLOX expects: scaled to fit size×size without distortion, padded with grey
// (114) right and below, values 0..255. It returns the scale applied. dst must
// hold 3*size*size floats and is overwritten.
func Letterbox(rgb []byte, w, h, size int, dst []float32) float32 {
	return LetterboxRect(rgb, w, h, 0, 0, w, h, size, dst)
}

// LetterboxRect is Letterbox for the region (rx, ry, rw, rh) of the frame.
func LetterboxRect(rgb []byte, w, h, rx, ry, rw, rh, size int, dst []float32) float32 {
	for i := range dst {
		dst[i] = 114
	}
	r := math.Min(float64(size)/float64(rw), float64(size)/float64(rh))
	nw, nh := int(float64(rw)*r), int(float64(rh)*r)
	if nw < 1 || nh < 1 {
		return float32(r)
	}
	plane := size * size
	// Bilinear sampling keeps thin things (people far away) from aliasing away
	// when a frame is shrunk; frames already at model size map 1:1.
	inv := 1 / r
	for y := 0; y < nh; y++ {
		sy := (float64(y)+0.5)*inv - 0.5
		y0 := int(math.Floor(sy))
		fy := float32(sy - float64(y0))
		y0c, y1c := ry+clamp(y0, rh-1), ry+clamp(y0+1, rh-1)
		for x := 0; x < nw; x++ {
			sx := (float64(x)+0.5)*inv - 0.5
			x0 := int(math.Floor(sx))
			fx := float32(sx - float64(x0))
			x0c, x1c := rx+clamp(x0, rw-1), rx+clamp(x0+1, rw-1)
			p00 := (y0c*w + x0c) * 3
			p01 := (y0c*w + x1c) * 3
			p10 := (y1c*w + x0c) * 3
			p11 := (y1c*w + x1c) * 3
			o := y*size + x
			for c := 0; c < 3; c++ {
				v00, v01 := float32(rgb[p00+c]), float32(rgb[p01+c])
				v10, v11 := float32(rgb[p10+c]), float32(rgb[p11+c])
				top := v00 + (v01-v00)*fx
				bot := v10 + (v11-v10)*fx
				// RGB in, BGR planes out.
				dst[(2-c)*plane+o] = top + (bot-top)*fy
			}
		}
	}
	return float32(r)
}

func clamp(v, hi int) int {
	if v < 0 {
		return 0
	}
	if v > hi {
		return hi
	}
	return v
}

// DecodeYOLOX turns a raw YOLOX output ([1, N, 5+classes], grid-relative, with
// objectness and class scores already through a sigmoid) into boxes normalised
// to the source frame. size is the model input side, scale what Letterbox
// returned, w×h the source frame. Boxes under minScore are dropped and
// overlapping boxes of a class are merged (non-maximum suppression).
func DecodeYOLOX(out []float32, dims []int64, size int, scale float32, w, h int, minScore float32) []Box {
	if len(dims) != 3 || dims[2] < 6 {
		return nil
	}
	rows, cols := int(dims[1]), int(dims[2])
	if len(out) < rows*cols {
		return nil
	}
	var boxes []Box
	idx := 0
	for _, stride := range []int{8, 16, 32} {
		g := size / stride
		for gy := 0; gy < g && idx < rows; gy++ {
			for gx := 0; gx < g && idx < rows; gx++ {
				p := out[idx*cols : (idx+1)*cols]
				idx++
				obj := p[4]
				if obj < minScore {
					continue
				}
				best, cls := float32(0), 0
				for c := 5; c < cols; c++ {
					if p[c] > best {
						best, cls = p[c], c-5
					}
				}
				score := obj * best
				if score < minScore {
					continue
				}
				st := float32(stride)
				cx := (p[0] + float32(gx)) * st
				cy := (p[1] + float32(gy)) * st
				bw := float32(math.Exp(float64(p[2]))) * st
				bh := float32(math.Exp(float64(p[3]))) * st
				// Model pixels → source pixels → normalised, clipped to the frame.
				x0 := clampF((cx-bw/2)/scale/float32(w), 0, 1)
				y0 := clampF((cy-bh/2)/scale/float32(h), 0, 1)
				x1 := clampF((cx+bw/2)/scale/float32(w), 0, 1)
				y1 := clampF((cy+bh/2)/scale/float32(h), 0, 1)
				if x1-x0 <= 0 || y1-y0 <= 0 {
					continue
				}
				boxes = append(boxes, Box{Class: cls, Score: score, X: x0, Y: y0, W: x1 - x0, H: y1 - y0})
			}
		}
	}
	return NMS(boxes, 0.45)
}

// Tiles splits a very wide frame into overlapping 16:9 regions, so people on a
// panoramic camera aren't shrunk to a few pixels. A normal frame is one tile.
// Each tile is {x, width}; all span the full height.
func Tiles(w, h int) [][2]int {
	if h <= 0 || float64(w)/float64(h) < 1.9 {
		return [][2]int{{0, w}}
	}
	tw := min(w, h*16/9)
	n := int(math.Ceil(float64(w) / float64(h) / 1.5))
	if n < 2 {
		n = 2
	}
	step := (w - tw) / (n - 1)
	out := make([][2]int, 0, n)
	for i := 0; i < n; i++ {
		x := i * step
		if i == n-1 {
			x = w - tw
		}
		out = append(out, [2]int{x, tw})
	}
	return out
}

// NMS keeps the best box among same-class boxes that overlap more than iou.
func NMS(boxes []Box, iou float32) []Box {
	sort.Slice(boxes, func(i, j int) bool { return boxes[i].Score > boxes[j].Score })
	keep := boxes[:0:0]
	for _, b := range boxes {
		ok := true
		for _, k := range keep {
			if k.Class == b.Class && IoU(k, b) > iou {
				ok = false
				break
			}
		}
		if ok {
			keep = append(keep, b)
		}
	}
	return keep
}

// IoU is the intersection over union of two boxes.
func IoU(a, b Box) float32 {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.W, b.X+b.W), min(a.Y+a.H, b.Y+b.H)
	if x1 <= x0 || y1 <= y0 {
		return 0
	}
	in := (x1 - x0) * (y1 - y0)
	return in / (a.W*a.H + b.W*b.H - in)
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// COCO is the label set YOLOX is trained on, in class order.
var COCO = []string{"person", "bicycle", "car", "motorcycle", "airplane", "bus", "train", "truck", "boat", "traffic light", "fire hydrant", "stop sign", "parking meter", "bench", "bird", "cat", "dog", "horse", "sheep", "cow", "elephant", "bear", "zebra", "giraffe", "backpack", "umbrella", "handbag", "tie", "suitcase", "frisbee", "skis", "snowboard", "sports ball", "kite", "baseball bat", "baseball glove", "skateboard", "surfboard", "tennis racket", "bottle", "wine glass", "cup", "fork", "knife", "spoon", "bowl", "banana", "apple", "sandwich", "orange", "broccoli", "carrot", "hot dog", "pizza", "donut", "cake", "chair", "couch", "potted plant", "bed", "dining table", "toilet", "tv", "laptop", "mouse", "remote", "keyboard", "cell phone", "microwave", "oven", "toaster", "sink", "refrigerator", "book", "clock", "vase", "scissors", "teddy bear", "hair drier", "toothbrush"}

// Label is the class name for a COCO class index (or PackageClass).
func Label(class int) string {
	if class == PackageClass {
		return "package"
	}
	if class >= 0 && class < len(COCO) {
		return COCO[class]
	}
	return "object"
}
