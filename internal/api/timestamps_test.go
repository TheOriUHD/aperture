package api

import (
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestCameraViewTimestampsAreMillis covers T1.7: cameraView (which previously
// exposed no timestamps) emits createdAtMs/updatedAtMs in unix milliseconds,
// converted from the seconds-stored columns.
func TestCameraViewTimestampsAreMillis(t *testing.T) {
	v := cameraView(&store.Camera{ID: "c", CreatedAt: 1_700_000_000, UpdatedAt: 1_700_000_005})
	if got := v["createdAtMs"]; got != int64(1_700_000_000_000) {
		t.Errorf("createdAtMs = %v, want 1700000000000", got)
	}
	if got := v["updatedAtMs"]; got != int64(1_700_000_005_000) {
		t.Errorf("updatedAtMs = %v, want 1700000005000", got)
	}
}
