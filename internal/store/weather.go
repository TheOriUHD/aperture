package store

import (
	"database/sql"
	"errors"
)

// WeatherSample is one recorded observation of ambient conditions. Every metric
// is a pointer so a sparse source (e.g. a PWS that reports only temp/RH) leaves
// the rest NULL rather than lying with zeros.
type WeatherSample struct {
	TS            int64    `json:"ts"`
	TempC         *float64 `json:"tempC,omitempty"`
	FeelsLikeC    *float64 `json:"feelsLikeC,omitempty"`
	HumidityPct   *float64 `json:"humidityPct,omitempty"`
	DewpointC     *float64 `json:"dewpointC,omitempty"`
	PressureHpa   *float64 `json:"pressureHpa,omitempty"`
	WindSpeedMs   *float64 `json:"windSpeedMs,omitempty"`
	WindGustMs    *float64 `json:"windGustMs,omitempty"`
	WindDirDeg    *float64 `json:"windDirDeg,omitempty"`
	PrecipMm      *float64 `json:"precipMm,omitempty"`
	PrecipProbPct *float64 `json:"precipProbPct,omitempty"`
	CloudPct      *float64 `json:"cloudPct,omitempty"`
	VisibilityM   *float64 `json:"visibilityM,omitempty"`
	UVIndex       *float64 `json:"uvIndex,omitempty"`
	ConditionCode *int     `json:"conditionCode,omitempty"`
	Source        string   `json:"source,omitempty"`
}

const weatherCols = `ts, temp_c, feels_like_c, humidity_pct, dewpoint_c, pressure_hpa,
	wind_speed_ms, wind_gust_ms, wind_dir_deg, precip_mm, precip_prob_pct,
	cloud_pct, visibility_m, uv_index, condition_code, source`

// InsertWeatherSample upserts one observation keyed by its timestamp, so a
// historical backfill that overlaps live polling is idempotent.
func (s *Store) InsertWeatherSample(ws WeatherSample) error {
	_, err := s.db.Exec(`INSERT INTO weather_samples (`+weatherCols+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(ts) DO UPDATE SET
			temp_c=excluded.temp_c, feels_like_c=excluded.feels_like_c,
			humidity_pct=excluded.humidity_pct, dewpoint_c=excluded.dewpoint_c,
			pressure_hpa=excluded.pressure_hpa, wind_speed_ms=excluded.wind_speed_ms,
			wind_gust_ms=excluded.wind_gust_ms, wind_dir_deg=excluded.wind_dir_deg,
			precip_mm=excluded.precip_mm, precip_prob_pct=excluded.precip_prob_pct,
			cloud_pct=excluded.cloud_pct, visibility_m=excluded.visibility_m,
			uv_index=excluded.uv_index, condition_code=excluded.condition_code,
			source=excluded.source`,
		ws.TS, ws.TempC, ws.FeelsLikeC, ws.HumidityPct, ws.DewpointC, ws.PressureHpa,
		ws.WindSpeedMs, ws.WindGustMs, ws.WindDirDeg, ws.PrecipMm, ws.PrecipProbPct,
		ws.CloudPct, ws.VisibilityM, ws.UVIndex, ws.ConditionCode, ws.Source)
	return err
}

// WeatherAt returns the most recent sample at-or-before ts (the conditions that
// were current at that instant). Returns ErrNotFound when no sample qualifies.
func (s *Store) WeatherAt(ts int64) (*WeatherSample, error) {
	row := s.db.QueryRow(`SELECT `+weatherCols+`
		FROM weather_samples WHERE ts <= ? ORDER BY ts DESC LIMIT 1`, ts)
	ws, err := scanWeather(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

// LatestWeather returns the newest recorded sample, or ErrNotFound.
func (s *Store) LatestWeather() (*WeatherSample, error) {
	row := s.db.QueryRow(`SELECT ` + weatherCols + ` FROM weather_samples ORDER BY ts DESC LIMIT 1`)
	ws, err := scanWeather(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

// WeatherRange returns samples with from <= ts <= to, oldest first.
func (s *Store) WeatherRange(from, to int64) ([]WeatherSample, error) {
	rows, err := s.db.Query(`SELECT `+weatherCols+`
		FROM weather_samples WHERE ts >= ? AND ts <= ? ORDER BY ts ASC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WeatherSample
	for rows.Next() {
		ws, err := scanWeather(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}

// CountWeather reports how many samples are stored (used to decide whether a
// first-run backfill is worthwhile).
func (s *Store) CountWeather() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM weather_samples`).Scan(&n)
	return n, err
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

func scanWeather(sc scanner) (WeatherSample, error) {
	var (
		ws                                            WeatherSample
		temp, feels, hum, dew, pres, ws10, gust, wdir sql.NullFloat64
		precip, pprob, cloud, vis, uv                 sql.NullFloat64
		code                                          sql.NullInt64
		src                                           sql.NullString
	)
	if err := sc.Scan(&ws.TS, &temp, &feels, &hum, &dew, &pres,
		&ws10, &gust, &wdir, &precip, &pprob, &cloud, &vis, &uv, &code, &src); err != nil {
		return ws, err
	}
	ws.TempC = fp(temp)
	ws.FeelsLikeC = fp(feels)
	ws.HumidityPct = fp(hum)
	ws.DewpointC = fp(dew)
	ws.PressureHpa = fp(pres)
	ws.WindSpeedMs = fp(ws10)
	ws.WindGustMs = fp(gust)
	ws.WindDirDeg = fp(wdir)
	ws.PrecipMm = fp(precip)
	ws.PrecipProbPct = fp(pprob)
	ws.CloudPct = fp(cloud)
	ws.VisibilityM = fp(vis)
	ws.UVIndex = fp(uv)
	if code.Valid {
		v := int(code.Int64)
		ws.ConditionCode = &v
	}
	ws.Source = src.String
	return ws, nil
}

func fp(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}
