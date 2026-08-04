package record

import (
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestScheduleMatchIsTimezoneSensitive covers T0.1: recording windows are
// authored against a wall clock, so whether a given instant falls inside a
// window depends on the timezone `now` is evaluated in. scheduledActive resolves
// `now` in the site-profile timezone (not the host's); this proves the matching
// itself is timezone-sensitive, which is exactly why that resolution matters.
func TestScheduleMatchIsTimezoneSensitive(t *testing.T) {
	// Window: Monday 09:00–17:00.
	scheds := []*store.Schedule{{Weekday: int(time.Monday), StartMin: 9 * 60, EndMin: 17 * 60}}

	// One absolute instant: Mon 2026-01-05 16:30 UTC — inside the window in UTC.
	inUTC := time.Date(2026, 1, 5, 16, 30, 0, 0, time.UTC)
	if !scheduleMatch(scheds, inUTC) {
		t.Fatalf("16:30 Mon UTC should be inside a Mon 09:00–17:00 window")
	}

	// The same moment in UTC+14 is Tue 06:30 — different weekday and hour, so it
	// must fall OUTSIDE. (FixedZone keeps this deterministic without tzdata.)
	east := time.FixedZone("UTC+14", 14*3600)
	if scheduleMatch(scheds, inUTC.In(east)) {
		t.Errorf("the same instant in UTC+14 is Tue 06:30 and must be OUTSIDE the window")
	}
}
