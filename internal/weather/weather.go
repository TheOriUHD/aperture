// Package weather records ambient conditions into a time-series so that
// scrubbing playback can reconstruct the weather *then*, not just show it *now*.
// The default provider is Open-Meteo: free, keyless, and — crucially — it has a
// historical archive, so a fresh install can backfill conditions for footage it
// already holds. The poller never touches the record path; it only writes rows.
package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/TheOriUHD/aperture/internal/site"
	"github.com/TheOriUHD/aperture/internal/store"
)

const (
	configKey       = "weather_config"
	defaultInterval = 10 * time.Minute
	minInterval     = time.Minute
	backfillDays    = 7
	httpTimeout     = 15 * time.Second
	sourceOpenMeteo = "open-meteo"
)

// Config is the persisted weather-source configuration.
type Config struct {
	Enabled     bool   `json:"enabled"`
	Source      string `json:"source"`      // currently only "open-meteo"
	IntervalMin int    `json:"intervalMin"` // poll cadence in minutes
}

func defaultConfig() Config { return Config{Enabled: true, Source: sourceOpenMeteo, IntervalMin: 10} }

// LoadConfig returns the stored config, defaulted when unset.
func LoadConfig(st *store.Store) Config {
	c := defaultConfig()
	if v, err := st.GetSetting(configKey); err == nil && v != "" {
		_ = json.Unmarshal([]byte(v), &c)
	}
	if c.Source == "" {
		c.Source = sourceOpenMeteo
	}
	if c.IntervalMin <= 0 {
		c.IntervalMin = 10
	}
	return c
}

// SaveConfig persists the config.
func SaveConfig(st *store.Store, c Config) error {
	if c.Source == "" {
		c.Source = sourceOpenMeteo
	}
	if c.IntervalMin <= 0 {
		c.IntervalMin = 10
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return st.SetSetting(configKey, string(b))
}

// Status is the live health of the poller, mirroring the HA status pill.
type Status struct {
	Configured  bool   `json:"configured"` // site location is set
	Enabled     bool   `json:"enabled"`
	Connected   bool   `json:"connected"` // last fetch succeeded
	LastUpdated int64  `json:"lastUpdated"`
	LastError   string `json:"lastError,omitempty"`
	Source      string `json:"source"`
}

// Poller fetches current conditions on a fixed cadence and writes one row per
// poll. Config and site changes take effect immediately via Reconcile.
type Poller struct {
	st  *store.Store
	log *slog.Logger
	hc  *http.Client

	reload chan struct{}

	mu            sync.Mutex
	status        Status
	latest        *store.WeatherSample
	backfilledKey string // "lat,lon" most recently backfilled
}

// New constructs a Poller.
func New(st *store.Store, log *slog.Logger) *Poller {
	return &Poller{
		st:     st,
		log:    log,
		hc:     &http.Client{Timeout: httpTimeout},
		reload: make(chan struct{}, 1),
	}
}

// Reconcile signals the poller to re-read config/site and poll immediately.
func (p *Poller) Reconcile() {
	select {
	case p.reload <- struct{}{}:
	default:
	}
}

// Status returns a snapshot of poller health.
func (p *Poller) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// Latest returns the most recently fetched sample, or nil.
func (p *Poller) Latest() *store.WeatherSample {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.latest
}

// Run polls until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	interval := p.interval()
	p.tick(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.reload:
			if ni := p.interval(); ni != interval {
				interval = ni
				t.Reset(interval)
			}
			p.tick(ctx)
		case <-t.C:
			p.tick(ctx)
		}
	}
}

func (p *Poller) interval() time.Duration {
	d := time.Duration(LoadConfig(p.st).IntervalMin) * time.Minute
	if d < minInterval {
		d = defaultInterval
	}
	return d
}

// tick performs one poll, updating status. It no-ops cleanly when the site is
// unconfigured or weather is disabled.
func (p *Poller) tick(ctx context.Context) {
	cfg := LoadConfig(p.st)
	loc := site.Load(p.st)

	st := Status{Configured: loc.Configured, Enabled: cfg.Enabled, Source: cfg.Source}
	if !loc.Configured || !cfg.Enabled {
		p.mu.Lock()
		p.status = st
		p.mu.Unlock()
		return
	}

	sample, err := p.fetchCurrent(ctx, loc.Lat, loc.Lon)
	if err != nil {
		st.LastError = err.Error()
		p.mu.Lock()
		p.status = st
		p.mu.Unlock()
		p.log.Warn("weather: fetch failed", "err", err)
		return
	}
	if err := p.st.InsertWeatherSample(sample); err != nil {
		p.log.Error("weather: store sample", "err", err)
	}

	st.Connected = true
	st.LastUpdated = sample.TS
	p.mu.Lock()
	p.status = st
	s := sample
	p.latest = &s
	p.mu.Unlock()

	// After the first successful poll for a location, backfill recent history so
	// existing footage gains context. Re-runs if the location changes; the
	// upserts make it idempotent on restart.
	key := fmt.Sprintf("%.3f,%.3f", loc.Lat, loc.Lon)
	p.mu.Lock()
	need := key != p.backfilledKey
	if need {
		p.backfilledKey = key
	}
	p.mu.Unlock()
	if need {
		go p.runBackfill(context.Background(), loc.Lat, loc.Lon)
	}
}

// ---- Open-Meteo provider ---------------------------------------------------

type omCurrent struct {
	Current struct {
		Time        int64    `json:"time"`
		Temp        *float64 `json:"temperature_2m"`
		Humidity    *float64 `json:"relative_humidity_2m"`
		Apparent    *float64 `json:"apparent_temperature"`
		Precip      *float64 `json:"precipitation"`
		WeatherCode *int     `json:"weather_code"`
		Cloud       *float64 `json:"cloud_cover"`
		Pressure    *float64 `json:"surface_pressure"`
		WindSpeed   *float64 `json:"wind_speed_10m"`
		WindDir     *float64 `json:"wind_direction_10m"`
		WindGust    *float64 `json:"wind_gusts_10m"`
	} `json:"current"`
}

func (p *Poller) fetchCurrent(ctx context.Context, lat, lon float64) (store.WeatherSample, error) {
	u := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.5f&longitude=%.5f"+
		"&current=temperature_2m,relative_humidity_2m,apparent_temperature,precipitation,"+
		"weather_code,cloud_cover,surface_pressure,wind_speed_10m,wind_direction_10m,wind_gusts_10m"+
		"&wind_speed_unit=ms&timeformat=unixtime&timezone=GMT", lat, lon)

	var resp omCurrent
	if err := p.getJSON(ctx, u, &resp); err != nil {
		return store.WeatherSample{}, err
	}
	c := resp.Current
	ts := c.Time
	if ts == 0 {
		ts = time.Now().Unix()
	}
	ws := store.WeatherSample{
		TS:            ts,
		TempC:         c.Temp,
		FeelsLikeC:    c.Apparent,
		HumidityPct:   c.Humidity,
		PressureHpa:   c.Pressure,
		WindSpeedMs:   c.WindSpeed,
		WindGustMs:    c.WindGust,
		WindDirDeg:    c.WindDir,
		PrecipMm:      c.Precip,
		CloudPct:      c.Cloud,
		ConditionCode: c.WeatherCode,
		Source:        sourceOpenMeteo,
	}
	if dp := dewpoint(c.Temp, c.Humidity); dp != nil {
		ws.DewpointC = dp
	}
	return ws, nil
}

type omArchive struct {
	Hourly struct {
		Time        []int64    `json:"time"`
		Temp        []*float64 `json:"temperature_2m"`
		Humidity    []*float64 `json:"relative_humidity_2m"`
		Apparent    []*float64 `json:"apparent_temperature"`
		Precip      []*float64 `json:"precipitation"`
		WeatherCode []*int     `json:"weather_code"`
		Cloud       []*float64 `json:"cloud_cover"`
		Pressure    []*float64 `json:"surface_pressure"`
		WindSpeed   []*float64 `json:"wind_speed_10m"`
		WindDir     []*float64 `json:"wind_direction_10m"`
		WindGust    []*float64 `json:"wind_gusts_10m"`
	} `json:"hourly"`
}

// runBackfill pulls recent hourly history and inserts any hour with a real
// temperature. It uses the forecast endpoint's past_days (not the archive API),
// which has no multi-day latency — so there is no gap between history and the
// live poll, and scrubbing back a few hours shows the right conditions.
func (p *Poller) runBackfill(ctx context.Context, lat, lon float64) {
	now := time.Now().Unix()
	u := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.5f&longitude=%.5f"+
		"&past_days=%d&forecast_days=1"+
		"&hourly=temperature_2m,relative_humidity_2m,apparent_temperature,precipitation,"+
		"weather_code,cloud_cover,surface_pressure,wind_speed_10m,wind_direction_10m,wind_gusts_10m"+
		"&wind_speed_unit=ms&timeformat=unixtime&timezone=GMT", lat, lon, backfillDays)

	var resp omArchive
	if err := p.getJSON(ctx, u, &resp); err != nil {
		p.log.Warn("weather: backfill fetch failed", "err", err)
		return
	}
	h := resp.Hourly
	inserted := 0
	for i, ts := range h.Time {
		if ts > now {
			continue // skip future forecast hours; the live poll owns "now"
		}
		temp := at(h.Temp, i)
		if temp == nil {
			continue // no real observation for this hour
		}
		ws := store.WeatherSample{
			TS:            ts,
			TempC:         temp,
			FeelsLikeC:    at(h.Apparent, i),
			HumidityPct:   at(h.Humidity, i),
			PressureHpa:   at(h.Pressure, i),
			WindSpeedMs:   at(h.WindSpeed, i),
			WindGustMs:    at(h.WindGust, i),
			WindDirDeg:    at(h.WindDir, i),
			PrecipMm:      at(h.Precip, i),
			CloudPct:      at(h.Cloud, i),
			ConditionCode: ati(h.WeatherCode, i),
			Source:        sourceOpenMeteo,
		}
		ws.DewpointC = dewpoint(temp, at(h.Humidity, i))
		if err := p.st.InsertWeatherSample(ws); err == nil {
			inserted++
		}
	}
	if inserted > 0 {
		p.log.Info("weather: backfilled history", "samples", inserted, "days", backfillDays)
	}
}

// ---- geocoding -------------------------------------------------------------

// GeoResult is a place candidate from the geocoder.
type GeoResult struct {
	Label      string  `json:"label"`
	Name       string  `json:"name"`
	Country    string  `json:"country"`
	Admin1     string  `json:"admin1"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	ElevationM float64 `json:"elevationM"`
	Timezone   string  `json:"timezone"`
}

// Geocode resolves a free-text place query to candidate locations via
// Open-Meteo's keyless geocoding API.
func (p *Poller) Geocode(ctx context.Context, q string) ([]GeoResult, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	u := "https://geocoding-api.open-meteo.com/v1/search?count=6&language=en&format=json&name=" + url.QueryEscape(q)
	var resp struct {
		Results []struct {
			Name      string  `json:"name"`
			Lat       float64 `json:"latitude"`
			Lon       float64 `json:"longitude"`
			Elevation float64 `json:"elevation"`
			Timezone  string  `json:"timezone"`
			Country   string  `json:"country"`
			Admin1    string  `json:"admin1"`
		} `json:"results"`
	}
	if err := p.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	out := make([]GeoResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		parts := []string{r.Name}
		if r.Admin1 != "" && r.Admin1 != r.Name {
			parts = append(parts, r.Admin1)
		}
		if r.Country != "" {
			parts = append(parts, r.Country)
		}
		out = append(out, GeoResult{
			Label:      strings.Join(parts, ", "),
			Name:       r.Name,
			Country:    r.Country,
			Admin1:     r.Admin1,
			Lat:        r.Lat,
			Lon:        r.Lon,
			ElevationM: r.Elevation,
			Timezone:   r.Timezone,
		})
	}
	return out, nil
}

// Resolve returns the timezone and elevation for a coordinate — used when the
// operator drags the map pin to a new spot, so the profile stays correct.
func (p *Poller) Resolve(ctx context.Context, lat, lon float64) (timezone string, elevationM float64, err error) {
	u := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.5f&longitude=%.5f"+
		"&timezone=auto&forecast_days=1&current=temperature_2m", lat, lon)
	var resp struct {
		Timezone  string  `json:"timezone"`
		Elevation float64 `json:"elevation"`
	}
	if err := p.getJSON(ctx, u, &resp); err != nil {
		return "", 0, err
	}
	return resp.Timezone, resp.Elevation, nil
}

// ---- helpers ---------------------------------------------------------------

func (p *Poller) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := p.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("provider returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// dewpoint computes the dew point (°C) from temperature and relative humidity
// via the Magnus formula. Returns nil if either input is missing/invalid.
func dewpoint(tempC, rh *float64) *float64 {
	if tempC == nil || rh == nil || *rh <= 0 || *rh > 100 {
		return nil
	}
	const a, b = 17.625, 243.04
	g := math.Log(*rh/100) + a**tempC/(b+*tempC)
	dp := b * g / (a - g)
	return &dp
}

func at(s []*float64, i int) *float64 {
	if i < 0 || i >= len(s) {
		return nil
	}
	return s[i]
}

func ati(s []*int, i int) *int {
	if i < 0 || i >= len(s) {
		return nil
	}
	return s[i]
}
