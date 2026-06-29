// Package astro computes deterministic solar and lunar events for a location and
// instant: sunrise/sunset, the civil/nautical/astronomical twilights, golden
// hour, solar noon, and moon phase/illumination. It is pure math — no network,
// no storage — so historical playback shading is exact for any past day for
// free. The algorithm is a faithful Go port of SunCalc (Vladimir Agafonkin),
// itself based on the Astronomy Answers formulae.
package astro

import (
	"math"
	"time"
)

const (
	rad       = math.Pi / 180
	j1970     = 2440588.0
	j2000     = 2451545.0
	obliquity = rad * 23.4397 // Earth's axial tilt
	j0        = 0.0009
)

// ---- date <-> julian -------------------------------------------------------

// toDays returns days since J2000 for the given instant.
func toDays(t time.Time) float64 {
	return float64(t.Unix())/86400.0 - 0.5 + j1970 - j2000
}

// fromJulian converts a full Julian Day Number to an instant (UTC).
func fromJulian(j float64) int64 {
	if math.IsNaN(j) {
		return 0
	}
	return int64(math.Round((j + 0.5 - j1970) * 86400.0))
}

// ---- general celestial math ------------------------------------------------

func solarMeanAnomaly(d float64) float64 { return rad * (357.5291 + 0.98560028*d) }

func eclipticLongitude(m float64) float64 {
	c := rad * (1.9148*math.Sin(m) + 0.02*math.Sin(2*m) + 0.0003*math.Sin(3*m)) // equation of center
	p := rad * 102.9372                                                         // perihelion of Earth
	return m + c + p + math.Pi
}

func declination(l, b float64) float64 {
	return math.Asin(math.Sin(b)*math.Cos(obliquity) + math.Cos(b)*math.Sin(obliquity)*math.Sin(l))
}

func rightAscension(l, b float64) float64 {
	return math.Atan2(math.Sin(l)*math.Cos(obliquity)-math.Tan(b)*math.Sin(obliquity), math.Cos(l))
}

func altitude(h, phi, dec float64) float64 {
	return math.Asin(math.Sin(phi)*math.Sin(dec) + math.Cos(phi)*math.Cos(dec)*math.Cos(h))
}

// ---- sun -------------------------------------------------------------------

func julianCycle(d, lw float64) float64 { return math.Round(d - j0 - lw/(2*math.Pi)) }

func approxTransit(ht, lw, n float64) float64 { return j0 + (ht+lw)/(2*math.Pi) + n }

func solarTransitJ(ds, m, l float64) float64 {
	return j2000 + ds + 0.0053*math.Sin(m) - 0.0069*math.Sin(2*l)
}

func hourAngle(h, phi, dec float64) float64 {
	return math.Acos((math.Sin(h) - math.Sin(phi)*math.Sin(dec)) / (math.Cos(phi) * math.Cos(dec)))
}

func getSetJ(h, lw, phi, dec, n, m, l float64) float64 {
	w := hourAngle(h, phi, dec)
	a := approxTransit(w, lw, n)
	return solarTransitJ(a, m, l)
}

// SunTimes holds a day's solar events as unix seconds (0 = does not occur, e.g.
// a twilight that never ends at high latitude in summer).
type SunTimes struct {
	SolarNoon     int64 `json:"solarNoon"`
	Nadir         int64 `json:"nadir"`
	Sunrise       int64 `json:"sunrise"`
	Sunset        int64 `json:"sunset"`
	SunriseEnd    int64 `json:"sunriseEnd"`
	SunsetStart   int64 `json:"sunsetStart"`
	Dawn          int64 `json:"dawn"`         // civil dawn (-6°)
	Dusk          int64 `json:"dusk"`         // civil dusk (-6°)
	NauticalDawn  int64 `json:"nauticalDawn"` // -12°
	NauticalDusk  int64 `json:"nauticalDusk"`
	NightEnd      int64 `json:"nightEnd"` // astronomical dawn (-18°)
	Night         int64 `json:"night"`    // astronomical dusk
	GoldenHourEnd int64 `json:"goldenHourEnd"`
	GoldenHour    int64 `json:"goldenHour"`
	AlwaysUp      bool  `json:"alwaysUp"`   // polar day: sun never sets
	AlwaysDown    bool  `json:"alwaysDown"` // polar night: sun never rises
}

// Sun computes the day's solar events for the instant t at the given latitude
// and longitude.
func Sun(t time.Time, lat, lng float64) SunTimes {
	lw := rad * -lng
	phi := rad * lat
	d := toDays(t)
	n := julianCycle(d, lw)
	ds := approxTransit(0, lw, n)
	m := solarMeanAnomaly(ds)
	l := eclipticLongitude(m)
	dec := declination(l, 0)
	jNoon := solarTransitJ(ds, m, l)

	out := SunTimes{
		SolarNoon: fromJulian(jNoon),
		Nadir:     fromJulian(jNoon - 0.5),
	}

	// each event is a symmetric pair around solar noon
	pair := func(angleDeg float64) (rise, set int64, ok bool) {
		jSet := getSetJ(angleDeg*rad, lw, phi, dec, n, m, l)
		if math.IsNaN(jSet) {
			return 0, 0, false
		}
		jRise := jNoon - (jSet - jNoon)
		return fromJulian(jRise), fromJulian(jSet), true
	}

	if r, s, ok := pair(-0.833); ok {
		out.Sunrise, out.Sunset = r, s
	} else {
		// Polar day/night: classify by the sun's altitude at solar noon.
		if altitude(0, phi, dec) > -0.833*rad {
			out.AlwaysUp = true
		} else {
			out.AlwaysDown = true
		}
	}
	if r, s, ok := pair(-0.3); ok {
		out.SunriseEnd, out.SunsetStart = r, s
	}
	if r, s, ok := pair(-6); ok {
		out.Dawn, out.Dusk = r, s
	}
	if r, s, ok := pair(-12); ok {
		out.NauticalDawn, out.NauticalDusk = r, s
	}
	if r, s, ok := pair(-18); ok {
		out.NightEnd, out.Night = r, s
	}
	if r, s, ok := pair(6); ok {
		out.GoldenHourEnd, out.GoldenHour = r, s
	}
	return out
}

// ---- moon ------------------------------------------------------------------

func moonCoords(d float64) (ra, dec, dist float64) {
	l := rad * (218.316 + 13.176396*d) // ecliptic longitude
	m := rad * (134.963 + 13.064993*d) // mean anomaly
	f := rad * (93.272 + 13.229350*d)  // mean distance

	lng := l + rad*6.289*math.Sin(m) // longitude
	lat := rad * 5.128 * math.Sin(f) // latitude
	dt := 385001 - 20905*math.Cos(m) // distance to moon, km
	return rightAscension(lng, lat), declination(lng, lat), dt
}

// MoonIllumination describes the moon's lit fraction and phase.
type MoonIllumination struct {
	// Fraction illuminated, 0..1.
	Fraction float64 `json:"fraction"`
	// Phase, 0..1: 0=new, 0.25=first quarter, 0.5=full, 0.75=last quarter.
	Phase float64 `json:"phase"`
	// Angle is the midpoint angle of the bright limb (radians).
	Angle float64 `json:"angle"`
	// Name is a human-readable phase label.
	Name string `json:"name"`
}

// Moon computes the moon's illumination at the instant t (location-independent
// to within a fraction of a percent, matching SunCalc).
func Moon(t time.Time) MoonIllumination {
	d := toDays(t)
	sM := solarMeanAnomaly(d)
	sL := eclipticLongitude(sM)
	sDec := declination(sL, 0)
	sRA := rightAscension(sL, 0)
	mRA, mDec, mDist := moonCoords(d)

	const sunDist = 149598000.0 // km
	phi := math.Acos(math.Sin(sDec)*math.Sin(mDec) + math.Cos(sDec)*math.Cos(mDec)*math.Cos(sRA-mRA))
	inc := math.Atan2(sunDist*math.Sin(phi), mDist-sunDist*math.Cos(phi))
	angle := math.Atan2(
		math.Cos(sDec)*math.Sin(sRA-mRA),
		math.Sin(sDec)*math.Cos(mDec)-math.Cos(sDec)*math.Sin(mDec)*math.Cos(sRA-mRA),
	)
	phase := 0.5 + 0.5*inc*sign(angle)/math.Pi
	mi := MoonIllumination{
		Fraction: (1 + math.Cos(inc)) / 2,
		Phase:    phase,
		Angle:    angle,
	}
	mi.Name = phaseName(phase)
	return mi
}

func sign(x float64) float64 {
	if x < 0 {
		return -1
	}
	return 1
}

// phaseName maps a 0..1 phase value to a label.
func phaseName(p float64) string {
	switch {
	case p < 0.03 || p > 0.97:
		return "new"
	case p < 0.22:
		return "waxing crescent"
	case p < 0.28:
		return "first quarter"
	case p < 0.47:
		return "waxing gibbous"
	case p < 0.53:
		return "full"
	case p < 0.72:
		return "waning gibbous"
	case p < 0.78:
		return "last quarter"
	default:
		return "waning crescent"
	}
}
