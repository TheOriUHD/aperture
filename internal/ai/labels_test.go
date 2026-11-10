package ai

import "testing"

func TestVocabMap(t *testing.T) {
	idx := func(names []string, name string) int {
		for i, n := range names {
			if n == name {
				return i
			}
		}
		t.Fatalf("no class %q", name)
		return -1
	}
	o365 := VocabMap(LabelsObjects365, Objects365)
	for name, want := range map[string]int{
		"Person": 0, "Car": 2, "SUV": 2, "Van": 2, "Truck": 7, "Pickup Truck": 7, "Bus": 5, "Motorcycle": 3,
		"Bicycle": 1, "Dog": 16, "Cat": 15, "Wild Bird": 14, "Horse": 17,
		"Storage box": PackageClass, "Luggage": PackageClass, "Backpack": PackageClass,
		"Sneakers": -1, "None": -1, "Lamp": -1,
	} {
		if got := o365[idx(Objects365, name)]; got != want {
			t.Errorf("Objects365 %q → %d, want %d", name, got, want)
		}
	}
	if !FindsPackages(o365) || FindsPackages(VocabMap(LabelsCOCO, COCO)) {
		t.Error("only Objects365 finds parcels")
	}
	// COCO by category id (RF-DETR): gaps report nothing.
	c91 := VocabMap(LabelsCOCO91, COCO91)
	if c91[1] != 0 || c91[3] != 2 || c91[12] != -1 || c91[90] != 79 {
		t.Errorf("coco91: person %d car %d gap %d toothbrush %d", c91[1], c91[3], c91[12], c91[90])
	}
	// A custom list: names and synonyms match, the rest is dropped.
	own := VocabMap("custom", []string{"Pedestrian", "parcel", "license plate", "Motorbike"})
	if own[0] != 0 || own[1] != PackageClass || own[2] != -1 || own[3] != 3 {
		t.Errorf("custom = %v", own)
	}
	pk := PackageVocab(o365)
	if pk[idx(Objects365, "Car")] != -1 || pk[idx(Objects365, "Person")] != 0 {
		t.Error("the package model reports more than people and parcels")
	}
}

func TestGuessFormat(t *testing.T) {
	if a, c := GuessFormat([]ProbeOutput{{Name: "output", Shape: []int64{1, 8400, 85}}}); a != "yolox" || c != 80 {
		t.Errorf("YOLOX: %s %d", a, c)
	}
	if a, c := GuessFormat([]ProbeOutput{{Name: "logits", Shape: []int64{1, 300, 366}}, {Name: "pred_boxes", Shape: []int64{1, 300, 4}}}); a != "detr" || c != 366 {
		t.Errorf("D-FINE: %s %d", a, c)
	}
	if a, c := GuessFormat([]ProbeOutput{{Name: "pred_boxes", Shape: []int64{1, 300, 4}}, {Name: "logits", Shape: []int64{1, 300, 91}}}); a != "detr" || c != 91 {
		t.Errorf("RF-DETR: %s %d", a, c)
	}
	if a, _ := GuessFormat([]ProbeOutput{{Name: "x", Shape: []int64{1, 1000}}}); a != "" {
		t.Errorf("a classifier taken for a detector: %s", a)
	}
	if set, _ := GuessLabels(366); set != LabelsObjects365 {
		t.Error("366 classes are Objects365")
	}
	if set, names := GuessLabels(7); set != "" || names != nil {
		t.Error("an unknown class count was guessed")
	}
}
