package ai

import "math"

// The package model is D-FINE trained on Objects365, whose 365 classes include
// the boxes and bags parcels look like; COCO's (YOLOX's) don't. It runs beside
// the main model, only now and then, on the cameras watching for packages.

// PackageClass is the class a package box carries in a Result (beside COCO's
// person, 0).
const PackageClass = 1000

// Objects365 class indices (index 0 is "None").
const o365Person = 1

// o365Package are the classes that count as a package. A parcel at the door
// comes out as any of them depending on its shape and wrapping (a soft
// crumpled one reads as luggage or a bag), so they are taken together.
var o365Package = [...]int{
	21,  // Storage box
	121, // Luggage
	195, // Briefcase
	14,  // Handbag/Satchel
	39,  // Backpack
}

// Stretch converts a region of a packed RGB24 frame into the planar RGB input
// D-FINE expects: resized to size×size without keeping the aspect ratio (as it
// was trained), values 0..1.
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

// DecodeDFINE turns D-FINE's outputs (logits [1,Q,C], boxes [1,Q,4] as centre
// x, centre y, width, height of the input, 0..1) into person and package boxes
// (top-left x, y, width, height, 0..1) scoring at least minScore. Each query is
// one object; for a package its score is the best of the package classes.
func DecodeDFINE(logits, boxes []float32, queries, classes int, minScore float32) []Box {
	if queries <= 0 || classes <= o365Package[len(o365Package)-1] || len(logits) < queries*classes || len(boxes) < queries*4 {
		return nil
	}
	sigmoid := func(v float32) float32 { return float32(1 / (1 + math.Exp(-float64(v)))) }
	var out []Box
	for q := 0; q < queries; q++ {
		row := logits[q*classes : (q+1)*classes]
		person := sigmoid(row[o365Person])
		var pkg float32
		for _, k := range o365Package {
			pkg = max(pkg, sigmoid(row[k]))
		}
		class, score := 0, person
		if pkg > person {
			class, score = PackageClass, pkg
		}
		if score < minScore {
			continue
		}
		b := boxes[q*4 : q*4+4]
		x, y := clamp01(b[0]-b[2]/2), clamp01(b[1]-b[3]/2)
		out = append(out, Box{Class: class, Score: score, X: x, Y: y, W: clamp01(b[0]+b[2]/2) - x, H: clamp01(b[1]+b[3]/2) - y})
	}
	// Queries rarely overlap, but the same parcel can be split across two
	// package classes.
	return NMS(out, 0.6)
}

func clamp01(v float32) float32 { return min(max(v, 0), 1) }
