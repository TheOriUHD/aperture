package ai

import "math"

// DETR-family models (D-FINE, RT-DETR, RF-DETR as exported by onnx-community)
// take a stretched RGB picture and answer with a fixed number of queries, each
// a box and a score per class. Aperture runs them as the main model (the
// all-in-one D-FINE-X on Objects365 finds parcels too) and as the separate
// package model (D-FINE-S on Objects365).

// PackageClass is the class a package box carries in a Result (beside the
// COCO classes).
const PackageClass = 1000

// Stretch converts a region of a packed RGB24 frame into the planar RGB input
// DETR models expect: resized to size×size without keeping the aspect ratio
// (as they were trained), values 0..1.
func Stretch(rgb []byte, w, h, rx, ry, rw, rh, size int, dst []float32) {
	plane := size * size
	sx := float64(rw) / float64(size)
	sy := float64(rh) / float64(size)
	for y := 0; y < size; y++ {
		fy := (float64(y)+0.5)*sy - 0.5
		y0 := int(math.Floor(fy))
		ty := float32(fy - float64(y0))
		y0c, y1c := ry+clamp(y0, rh-1), ry+clamp(y0+1, rh-1)
		for x := 0; x < size; x++ {
			fx := (float64(x)+0.5)*sx - 0.5
			x0 := int(math.Floor(fx))
			tx := float32(fx - float64(x0))
			x0c, x1c := rx+clamp(x0, rw-1), rx+clamp(x0+1, rw-1)
			p00 := (y0c*w + x0c) * 3
			p01 := (y0c*w + x1c) * 3
			p10 := (y1c*w + x0c) * 3
			p11 := (y1c*w + x1c) * 3
			o := y*size + x
			for c := 0; c < 3; c++ {
				v00, v01 := float32(rgb[p00+c]), float32(rgb[p01+c])
				v10, v11 := float32(rgb[p10+c]), float32(rgb[p11+c])
				top := v00 + (v01-v00)*tx
				bot := v10 + (v11-v10)*tx
				dst[c*plane+o] = (top + (bot-top)*ty) / 255
			}
		}
	}
}

// DecodeDETR turns a DETR model's outputs (logits [1,Q,C], boxes [1,Q,4] as
// centre x, centre y, width, height of the input, 0..1) into boxes (top-left
// x, y, width, height, 0..1) of Aperture's classes scoring at least minScore.
// vocab maps each model class to Aperture's (see VocabMap; -1 = not
// reported). Each query is one object, and its class is the best of the
// reported ones: a parcel scoring 0.3 as "storage box" and 0.5 as a picture
// frame is still a parcel.
func DecodeDETR(logits, boxes []float32, queries, classes int, vocab []int, minScore float32) []Box {
	if queries <= 0 || classes <= 0 || len(vocab) < classes || len(logits) < queries*classes || len(boxes) < queries*4 {
		return nil
	}
	var out []Box
	for q := 0; q < queries; q++ {
		row := logits[q*classes : (q+1)*classes]
		class, best := -1, float32(math.Inf(-1))
		for k, v := range vocab[:classes] {
			if v >= 0 && row[k] > best {
				class, best = v, row[k]
			}
		}
		if class < 0 {
			continue
		}
		score := float32(1 / (1 + math.Exp(-float64(best))))
		if score < minScore {
			continue
		}
		b := boxes[q*4 : q*4+4]
		x, y := clamp01(b[0]-b[2]/2), clamp01(b[1]-b[3]/2)
		out = append(out, Box{Class: class, Score: score, X: x, Y: y, W: clamp01(b[0]+b[2]/2) - x, H: clamp01(b[1]+b[3]/2) - y})
	}
	// Queries rarely overlap, but the same object can be split across two
	// classes that map to one label.
	return NMS(out, 0.6)
}

func clamp01(v float32) float32 { return min(max(v, 0), 1) }
