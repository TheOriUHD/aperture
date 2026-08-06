package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/TheOriUHD/aperture/internal/astro"
	"github.com/TheOriUHD/aperture/internal/site"
	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/weather"
)

// ---- site profile ----------------------------------------------------------

// handleGetSite returns the geographic profile. Readable by any authenticated
// user (the overlay and timeline shading need units/timezone/coordinates).
func (s *Server) handleGetSite(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, site.Load(s.st))
}

// handleSetSite persists the geographic profile (admin) and repolls weather.
func (s *Server) handleSetSite(w http.ResponseWriter, r *http.Request) {
	var req site.Site
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := site.Save(s.st, req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.weather != nil {
		s.weather.Reconcile()
	}
	s.logAudit(r, "site.update", req.AddressLabel, "")
	s.emitConfig("site")
	writeJSON(w, http.StatusOK, site.Load(s.st))
}

// handleGeocode resolves a free-text place query to candidate locations (admin
// setup helper).
func (s *Server) handleGeocode(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, err := s.weather.Geocode(ctx, q)
	if err != nil {
		writeError(w, http.StatusBadGateway, "geocoding unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": res})
}

// handleResolveGeo returns timezone + elevation for a coordinate (admin map-pin
// drag helper).
func (s *Server) handleResolveGeo(w http.ResponseWriter, r *http.Request) {
	lat, err1 := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lon, err2 := strconv.ParseFloat(r.URL.Query().Get("lon"), 64)
	if err1 != nil || err2 != nil {
		writeError(w, http.StatusBadRequest, "invalid coordinates")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	tz, elev, err := s.weather.Resolve(ctx, lat, lon)
	if err != nil {
		writeError(w, http.StatusBadGateway, "lookup unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"timezone": tz, "elevationM": elev})
}

// ---- weather ---------------------------------------------------------------

// handleGetWeather returns the weather-source config plus live status and the
// current sample (admin).
func (s *Server) handleGetWeather(w http.ResponseWriter, r *http.Request) {
	cfg := weather.LoadConfig(s.st)
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":     cfg.Enabled,
		"source":      cfg.Source,
		"intervalMin": cfg.IntervalMin,
		"status":      s.weather.Status(),
		"current":     s.weather.Latest(),
	})
}

// handleSetWeather updates the weather-source config (admin) and repolls.
func (s *Server) handleSetWeather(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled     bool   `json:"enabled"`
		Source      string `json:"source"`
		IntervalMin int    `json:"intervalMin"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := weather.Config{Enabled: req.Enabled, Source: req.Source, IntervalMin: req.IntervalMin}
	if err := weather.SaveConfig(s.st, cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.weather.Reconcile()
	s.logAudit(r, "weather.update", req.Source, "enabled="+boolStr(req.Enabled))
	s.handleGetWeather(w, r)
}

// handleWeatherNow returns the current conditions sample and freshness, for the
// live overlay. Any authenticated user.
func (s *Server) handleWeatherNow(w http.ResponseWriter, r *http.Request) {
	st := s.weather.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"sample":      s.weather.Latest(),
		"connected":   st.Connected,
		"lastUpdated": st.LastUpdated,
		"source":      st.Source,
		"configured":  st.Configured,
	})
}

// handleWeatherAt returns the sample current at ?ts= (unix seconds; defaults to
// now), i.e. the conditions at the playhead.
func (s *Server) handleWeatherAt(w http.ResponseWriter, r *http.Request) {
	ts := time.Now().Unix()
	if v := r.URL.Query().Get("ts"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid ts")
			return
		}
		ts = n
	}
	sample, err := s.st.WeatherAt(ts)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sample)
}

// handleWeatherRange returns samples within [from,to] (unix seconds) for the
// timeline ribbon.
func (s *Server) handleWeatherRange(w http.ResponseWriter, r *http.Request) {
	from, err1 := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	to, err2 := strconv.ParseInt(r.URL.Query().Get("to"), 10, 64)
	if err1 != nil || err2 != nil || to <= from {
		writeError(w, http.StatusBadRequest, "invalid range")
		return
	}
	samples, err := s.st.WeatherRange(from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if samples == nil {
		samples = []store.WeatherSample{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"samples": samples})
}

// ---- astronomy -------------------------------------------------------------

// handleAstro returns sun and moon events for ?date=YYYY-MM-DD (defaults to
// today in the site timezone). Returns {configured:false} when no site is set.
func (s *Server) handleAstro(w http.ResponseWriter, r *http.Request) {
	loc := site.Load(s.st)
	if !loc.Configured {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	tz, err := time.LoadLocation(loc.Timezone)
	if err != nil {
		tz = time.UTC
	}
	day := time.Now().In(tz)
	if v := r.URL.Query().Get("date"); v != "" {
		d, err := time.ParseInLocation("2006-01-02", v, tz)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid date")
			return
		}
		day = d
	}
	// Local noon is a stable representative instant for the day's events.
	noon := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, tz)
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": true,
		"date":       noon.Format("2006-01-02"),
		"timezone":   loc.Timezone,
		"sun":        astro.Sun(noon, loc.Lat, loc.Lon),
		"moon":       astro.Moon(noon),
	})
}
