-- Context & environmental layer.
--
-- The site profile (lat/lon/elevation/timezone/units) and the weather-source
-- configuration both live as JSON blobs in the existing key/value `settings`
-- table, so they need no schema here. This migration adds only the weather
-- time-series: one row per poll, looked up as "nearest sample at-or-before ts"
-- (a single indexed query against the integer primary key). Rows are a few
-- hundred bytes, so years of history fit in megabytes.

CREATE TABLE IF NOT EXISTS weather_samples (
    ts              INTEGER PRIMARY KEY,   -- unix seconds, aligned to the poll
    temp_c          REAL,
    feels_like_c    REAL,
    humidity_pct    REAL,
    dewpoint_c      REAL,
    pressure_hpa    REAL,
    wind_speed_ms   REAL,
    wind_gust_ms    REAL,
    wind_dir_deg    REAL,
    precip_mm       REAL,
    precip_prob_pct REAL,
    precip_type     TEXT,
    cloud_pct       REAL,
    visibility_m    REAL,
    uv_index        REAL,
    aqi             INTEGER,
    condition_code  INTEGER,                -- WMO weather code, source-normalized
    source          TEXT
);
