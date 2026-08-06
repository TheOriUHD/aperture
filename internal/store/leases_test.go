package store

import (
	"path/filepath"
	"testing"
)

// TestLeaseOverlapAndExpiry pins the half-open overlap test and expiry handling
// used by the janitor.
func TestLeaseOverlapAndExpiry(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	const now = int64(1_000_000)
	l, err := st.CreateLease("cam1", 1000, 2000, now+1000, "h")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	cases := []struct {
		name             string
		cam              string
		segStart, segEnd int64
		at               int64
		want             bool
	}{
		{"overlap-middle", "cam1", 1500, 1600, now, true},
		{"overlap-straddle-start", "cam1", 900, 1100, now, true},
		{"touch-end-excluded", "cam1", 2000, 2100, now, false}, // half-open: to_ts is exclusive
		{"touch-start-excluded", "cam1", 500, 1000, now, false},
		{"other-camera", "cam2", 1500, 1600, now, false},
		{"after-expiry", "cam1", 1500, 1600, now + 2000, false},
	}
	for _, c := range cases {
		got, err := st.LeaseOverlaps(c.cam, c.segStart, c.segEnd, c.at)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: LeaseOverlaps = %v, want %v", c.name, got, c.want)
		}
	}

	// HasActiveLeases + purge.
	if ok, _ := st.HasActiveLeases(now); !ok {
		t.Error("HasActiveLeases should be true before expiry")
	}
	if err := st.PurgeExpiredLeases(now + 2000); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := st.GetLease(l.ID); err == nil {
		t.Error("expired lease should be purged")
	}
	if ok, _ := st.HasActiveLeases(now + 2000); ok {
		t.Error("HasActiveLeases should be false after purge")
	}
}
