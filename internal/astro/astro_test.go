package astro

import (
	"math"
	"testing"
	"time"
)

// Reference values from the SunCalc test suite for 2013-03-05 UTC at 50.5°N
// 30.5°E. We assert to within a minute (our rounding) for sun events and within
// 1% for moon illumination.
func TestSunReference(t *testing.T) {
	when := time.Unix(1362441600, 0).UTC() // 2013-03-05T00:00:00Z
	got := Sun(when, 50.5, 30.5)

	cases := []struct {
		name string
		got  int64
		want int64 // unix seconds of the SunCalc reference instant
	}{
		{"solarNoon", got.SolarNoon, 1362478257},       // 10:10:57Z
		{"sunrise", got.Sunrise, 1362458096},           // 04:34:56Z
		{"sunset", got.Sunset, 1362498417},             // 15:46:57Z
		{"dawn", got.Dawn, 1362456137},                 // 04:02:17Z
		{"dusk", got.Dusk, 1362500376},                 // 16:19:36Z
		{"goldenHour", got.GoldenHour, 1362495772},     // 15:02:52Z
		{"nauticalDawn", got.NauticalDawn, 1362453871}, // 03:24:31Z
	}
	for _, c := range cases {
		if d := c.got - c.want; d < -60 || d > 60 {
			t.Errorf("%s = %d, want ~%d (off by %ds)", c.name, c.got, c.want, d)
		}
	}
}

func TestMoonReference(t *testing.T) {
	when := time.Unix(1362441600, 0).UTC() // 2013-03-05T00:00:00Z
	mi := Moon(when)
	if math.Abs(mi.Fraction-0.4848) > 0.02 {
		t.Errorf("moon fraction = %.4f, want ~0.4848", mi.Fraction)
	}
	if math.Abs(mi.Phase-0.7567) > 0.02 {
		t.Errorf("moon phase = %.4f, want ~0.7567", mi.Phase)
	}
}

// A high-latitude summer day must classify as polar day rather than crash.
func TestPolarDay(t *testing.T) {
	when := time.Date(2013, 6, 21, 12, 0, 0, 0, time.UTC)
	got := Sun(when, 80, 0) // 80°N midsummer: sun never sets
	if !got.AlwaysUp {
		t.Errorf("expected AlwaysUp at 80N midsummer, got %+v", got)
	}
	if got.Sunset != 0 || got.Sunrise != 0 {
		t.Errorf("expected zero sunrise/sunset in polar day, got rise=%d set=%d", got.Sunrise, got.Sunset)
	}
}
