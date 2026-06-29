// Package site holds the NVR's geographic profile — the single source of truth
// for location, timezone, and unit preferences that every context-layer feature
// (weather, sun/moon, day-night timeline shading) reads. When the profile is
// unset, downstream features must no-op cleanly rather than crash.
package site

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/TheOriUHD/aperture/internal/store"
)

const settingKey = "site_profile"

// Units captures the operator's display-unit choices. The 12/24-hour clock is a
// separate, app-wide user preference, deliberately not duplicated here.
type Units struct {
	Temp     string `json:"temp"`     // "c" | "f"
	Wind     string `json:"wind"`     // "ms" | "kmh" | "mph" | "kn"
	Pressure string `json:"pressure"` // "hpa" | "inhg"
}

// DefaultUnits is the metric baseline.
func DefaultUnits() Units { return Units{Temp: "c", Wind: "ms", Pressure: "hpa"} }

// Site is the geographic + units profile. Lat/Lon are the linchpin: when
// Configured is false, every context feature hides itself.
type Site struct {
	Configured   bool    `json:"configured"`
	Lat          float64 `json:"lat"`
	Lon          float64 `json:"lon"`
	ElevationM   float64 `json:"elevationM"`
	Timezone     string  `json:"timezone"`
	AddressLabel string  `json:"addressLabel"`
	Units        Units   `json:"units"`
}

// Load returns the stored site profile, or a zero (unconfigured) profile with
// default units so callers can always read Units safely.
func Load(st *store.Store) Site {
	s := Site{Units: DefaultUnits()}
	v, err := st.GetSetting(settingKey)
	if err != nil || v == "" {
		return s
	}
	_ = json.Unmarshal([]byte(v), &s)
	if s.Units.Temp == "" {
		s.Units.Temp = "c"
	}
	if s.Units.Wind == "" {
		s.Units.Wind = "ms"
	}
	if s.Units.Pressure == "" {
		s.Units.Pressure = "hpa"
	}
	return s
}

// Save validates and persists the site profile, stamping Configured=true.
func Save(st *store.Store, s Site) error {
	if s.Lat < -90 || s.Lat > 90 || s.Lon < -180 || s.Lon > 180 {
		return errors.New("latitude/longitude out of range")
	}
	if s.Lat == 0 && s.Lon == 0 {
		return errors.New("latitude/longitude required")
	}
	if strings.TrimSpace(s.Timezone) == "" {
		s.Timezone = "UTC"
	}
	if s.Units.Temp == "" {
		s.Units = DefaultUnits()
	}
	s.Configured = true
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return st.SetSetting(settingKey, string(b))
}
