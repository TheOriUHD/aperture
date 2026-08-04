package motion

import (
	"encoding/json"
	"testing"
)

// TestRegionDiffAndMask proves the core of region-aware detection: a changed
// region is detected, and the same change is suppressed when its area is masked.
func TestRegionDiffAndMask(t *testing.T) {
	n := gridW * gridH
	prev := make([]byte, n)
	cur := make([]byte, n)
	// A bright block in the left ~16% of the frame (detection cols 0..9).
	for y := 0; y < gridH; y++ {
		for x := 0; x < 10; x++ {
			cur[y*gridW+x] = 220
		}
	}

	noMask := make([]bool, n)
	if frac := diffFrac(prev, cur, noMask, n, nil); frac <= 0 {
		t.Fatalf("expected motion with no mask, got %f", frac)
	}

	// Mask editor cols 0..15 across all rows — covers the changed region.
	var ign []int
	for r := 0; r < 18; r++ {
		for c := 0; c < 16; c++ {
			ign = append(ign, r*32+c)
		}
	}
	b, _ := json.Marshal(map[string]any{"cols": 32, "rows": 18, "ignore": ign})
	mask := parseIgnoreMask(string(b))
	active := n - countTrue(mask)
	if frac := diffFrac(prev, cur, mask, active, nil); frac != 0 {
		t.Fatalf("masked region should yield no motion, got %f", frac)
	}

	// A change in the right half must still register with that left mask.
	for y := 0; y < gridH; y++ {
		cur[y*gridW+gridW-1] = 220
	}
	if frac := diffFrac(prev, cur, mask, active, nil); frac <= 0 {
		t.Fatalf("unmasked change should still register, got %f", frac)
	}
}
