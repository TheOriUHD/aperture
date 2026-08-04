package motion

import "encoding/json"

const (
	gridW    = 64 // detection grid width
	gridH    = 36 // detection grid height
	pixNoise = 24 // per-pixel luma delta counted as "changed"

	// The coarse grid stored per event (and drawn on by Grid Search). 32×18 keeps
	// it compact and matches the zone editor's aspect for a shared mental model.
	SearchW = 32
	SearchH = 18
)

// motionMask is the persisted ignore-region for a camera: a coarse editor grid
// whose painted cells are excluded from motion detection.
type motionMask struct {
	Cols   int   `json:"cols"`
	Rows   int   `json:"rows"`
	Ignore []int `json:"ignore"` // flat editor cell indices (row*cols+col) to ignore
}

// parseIgnoreMask returns a gridW*gridH bool slice (true = ignore this pixel),
// mapping the (coarser) editor grid onto the detection grid.
func parseIgnoreMask(zonesJSON string) []bool {
	out := make([]bool, gridW*gridH)
	if zonesJSON == "" {
		return out
	}
	var m motionMask
	if json.Unmarshal([]byte(zonesJSON), &m) != nil || m.Cols <= 0 || m.Rows <= 0 || len(m.Ignore) == 0 {
		return out
	}
	ign := make(map[int]bool, len(m.Ignore))
	for _, i := range m.Ignore {
		ign[i] = true
	}
	for y := 0; y < gridH; y++ {
		ey := y * m.Rows / gridH
		for x := 0; x < gridW; x++ {
			ex := x * m.Cols / gridW
			if ign[ey*m.Cols+ex] {
				out[y*gridW+x] = true
			}
		}
	}
	return out
}

// diffFrac returns the fraction of unmasked cells whose luma changed beyond the
// noise threshold between two grayscale frames. If acc (len SearchW*SearchH) is
// non-nil, each changed cell also marks its coarse search cell true — the raw
// material for the per-event motion map used by Grid Search.
func diffFrac(prev, cur []byte, mask []bool, active int, acc []bool) float64 {
	if active <= 0 {
		return 0
	}
	changed := 0
	n := len(cur)
	if len(prev) < n {
		n = len(prev)
	}
	for i := 0; i < n; i++ {
		if i < len(mask) && mask[i] {
			continue
		}
		d := int(cur[i]) - int(prev[i])
		if d < 0 {
			d = -d
		}
		if d > pixNoise {
			changed++
			if acc != nil {
				cx := (i % gridW) * SearchW / gridW
				cy := (i / gridW) * SearchH / gridH
				acc[cy*SearchW+cx] = true
			}
		}
	}
	return float64(changed) / float64(active)
}

func countTrue(b []bool) int {
	n := 0
	for _, v := range b {
		if v {
			n++
		}
	}
	return n
}
