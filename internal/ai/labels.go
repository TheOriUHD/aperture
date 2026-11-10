package ai

import "strings"

// Aperture speaks one label vocabulary: COCO's 80 classes (a Box's Class is
// the COCO index) plus PackageClass. Models with other class lists, such as
// Objects365 or a custom model's own, are mapped onto it by name; classes
// with no counterpart are not reported.

// Built-in label sets.
const (
	LabelsCOCO       = "coco80" // COCO's 80 classes, in COCO order (YOLOX, D-FINE COCO)
	LabelsObjects365 = "obj365" // Objects365's 365 classes, index 0 "None" (D-FINE Objects365)
)

// ClassNames is the class list of a built-in label set (nil if unknown).
func ClassNames(set string) []string {
	switch set {
	case LabelsCOCO, "":
		return COCO
	case LabelsCOCO91:
		return COCO91
	case LabelsObjects365:
		return Objects365
	}
	return nil
}

// synonyms are other models' names for Aperture's labels.
var synonyms = map[string]string{
	// people
	"people": "person", "pedestrian": "person", "human": "person", "man": "person", "woman": "person",
	"child": "person", "boy": "person", "girl": "person",
	// vehicles
	"suv": "car", "sports car": "car", "van": "car", "taxi": "car", "sedan": "car", "automobile": "car",
	"pickup truck": "truck", "heavy truck": "truck", "fire truck": "truck", "ambulance": "truck", "lorry": "truck",
	"motorbike": "motorcycle", "scooter": "motorcycle", "moped": "motorcycle",
	"bike": "bicycle", "tricycle": "bicycle",
	// animals
	"wild bird": "bird", "pigeon": "bird", "duck": "bird", "goose": "bird", "parrot": "bird", "chicken": "bird",
	"puppy": "dog", "kitten": "cat",
	// VOC-style spellings of COCO classes (D-FINE's COCO export uses them)
	"aeroplane": "airplane", "tvmonitor": "tv", "pottedplant": "potted plant", "diningtable": "dining table",
	"sofa": "couch",
	// parcels
	"package": "package", "parcel": "package", "box": "package", "cardboard box": "package", "carton": "package",
	"storage box": "package", "delivery box": "package",
}

// o365Packages are Objects365's classes a parcel at the door comes out as
// (a soft crumpled one reads as luggage or a bag): taken together as one.
var o365Packages = map[string]bool{
	"Storage box": true, "Luggage": true, "Briefcase": true, "Handbag/Satchel": true, "Backpack": true,
}

var cocoIndex = func() map[string]int {
	m := make(map[string]int, len(COCO))
	for i, n := range COCO {
		m[n] = i
	}
	return m
}()

// VocabMap maps a model's class list onto Aperture's classes: for each class
// index, the COCO index, PackageClass, or -1 (not reported). set is the label
// set the names come from (LabelsCOCO maps by position).
func VocabMap(set string, names []string) []int {
	out := make([]int, len(names))
	for i, n := range names {
		out[i] = -1
		if set == LabelsCOCO && len(names) == len(COCO) {
			out[i] = i
			continue
		}
		if set == LabelsObjects365 && o365Packages[n] {
			out[i] = PackageClass
			continue
		}
		key := strings.ToLower(strings.TrimSpace(n))
		if s, ok := synonyms[key]; ok {
			key = s
		}
		if key == "package" {
			out[i] = PackageClass
		} else if c, ok := cocoIndex[key]; ok {
			out[i] = c
		}
	}
	return out
}

// PackageVocab keeps only people and parcels from a vocabulary map (what the
// separate package model reports).
func PackageVocab(vocab []int) []int {
	out := make([]int, len(vocab))
	for i, v := range vocab {
		out[i] = -1
		if v == 0 || v == PackageClass {
			out[i] = v
		}
	}
	return out
}

// FindsPackages reports whether a vocabulary map includes parcels.
func FindsPackages(vocab []int) bool {
	for _, v := range vocab {
		if v == PackageClass {
			return true
		}
	}
	return false
}

// LabelsCOCO91 is COCO numbered by its original category ids (1-90, with
// gaps; index 0 is "N/A"), as RF-DETR and other DETR exports number them.
const LabelsCOCO91 = "coco91"

// COCO91 is the COCO class list indexed by category id.
var COCO91 = func() []string {
	gaps := map[int]bool{12: true, 26: true, 29: true, 30: true, 45: true, 66: true, 68: true, 69: true, 71: true, 83: true}
	out := make([]string, 91)
	out[0] = "N/A"
	next := 0
	for id := 1; id <= 90; id++ {
		if gaps[id] {
			out[id] = "N/A"
			continue
		}
		out[id] = COCO[next]
		next++
	}
	return out
}()

// GuessLabels names the label set a model with this many classes most likely
// uses: COCO (80 or 91 by category id) or Objects365 (366 with "None" first,
// or 365 without). Other counts need the operator's class list.
func GuessLabels(classes int) (set string, names []string) {
	switch classes {
	case 80:
		return LabelsCOCO, COCO
	case 91:
		return LabelsCOCO91, COCO91
	case 366:
		return LabelsObjects365, Objects365
	case 365:
		return "custom", Objects365[1:]
	}
	return "", nil
}
