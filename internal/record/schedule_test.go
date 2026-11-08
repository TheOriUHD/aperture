package record

import (
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/config"
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

// Everything that calls a camera broken asks this. A Monday 07:00-23:00 camera at
// 03:00 is idle, not failing, and neither is it at 07:01, when the window has just
// opened and the newest footage is still from last night.
func TestRecordingDue(t *testing.T) {
	r, st := newRecorder(t, &config.Config{})
	if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "c1", RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSchedules("c1", []*store.Schedule{{Weekday: int(time.Monday), StartMin: 7 * 60, EndMin: 23 * 60, Mode: store.ModeContinuous}}); err != nil {
		t.Fatal(err)
	}
	at := func(h, m int) time.Time { return time.Date(2026, 1, 5, h, m, 0, 0, time.Local) } // a Monday
	scheduled := &store.Camera{ID: "c1", Enabled: true, RecordMode: store.ModeScheduled}

	for _, c := range []struct {
		name string
		cam  *store.Camera
		now  time.Time
		want bool
	}{
		{"before the window", scheduled, at(3, 0), false},
		{"window just opened", scheduled, at(7, 1), false},
		{"grace still running", scheduled, at(7, 2), false},
		{"grace over", scheduled, at(7, 3), true},
		{"mid window", scheduled, at(12, 0), true},
		{"last minute", scheduled, at(22, 59), true},
		{"window closed", scheduled, at(23, 0), false},
		{"another day", scheduled, at(12, 0).AddDate(0, 0, 1), false},
		{"scheduled with no schedule", &store.Camera{ID: "other", Enabled: true, RecordMode: store.ModeScheduled}, at(12, 0), false},
		{"continuous", &store.Camera{ID: "c1", Enabled: true, RecordMode: store.ModeContinuous}, at(3, 0), true},
		{"motion", &store.Camera{ID: "c1", Enabled: true, RecordMode: store.ModeMotion}, at(3, 0), true},
		{"set to never", &store.Camera{ID: "c1", Enabled: true, RecordMode: store.ModeNever}, at(12, 0), false},
		{"disabled", &store.Camera{ID: "c1", RecordMode: store.ModeContinuous}, at(12, 0), false},
	} {
		if got := r.recordingDueAt(c.cam, c.now); got != c.want {
			t.Errorf("%s: due = %v, want %v", c.name, got, c.want)
		}
	}
}
