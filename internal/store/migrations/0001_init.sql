-- Initial Aperture schema.
-- Time conventions: *_ts fields (segments/events) are unix milliseconds;
-- created_at/updated_at are unix seconds.

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL DEFAULT '',
    password_hash TEXT,                 -- null for OIDC-only accounts
    role          TEXT NOT NULL DEFAULT 'viewer',  -- 'admin' | 'viewer'
    oidc_sub      TEXT,                 -- subject from the OIDC provider, if any
    disabled      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_users_oidc_sub ON users(oidc_sub) WHERE oidc_sub IS NOT NULL;

CREATE TABLE cameras (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    rtsp_main_url       TEXT NOT NULL,
    rtsp_sub_url        TEXT NOT NULL DEFAULT '',
    enabled             INTEGER NOT NULL DEFAULT 1,
    record_mode         TEXT NOT NULL DEFAULT 'continuous', -- never|continuous|motion|scheduled
    retention_days      INTEGER NOT NULL DEFAULT 7,
    storage_quota_bytes INTEGER NOT NULL DEFAULT 0,         -- 0 = no per-camera cap
    motion_threshold    REAL NOT NULL DEFAULT 0.02,
    motion_zones_json   TEXT NOT NULL DEFAULT '',
    fps                 INTEGER NOT NULL DEFAULT 0,         -- 0 = source default
    sort_order          INTEGER NOT NULL DEFAULT 0,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
);

CREATE TABLE user_camera_acl (
    user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    camera_id TEXT NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, camera_id)
);

CREATE TABLE segments (
    id          TEXT PRIMARY KEY,
    camera_id   TEXT NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    start_ts    INTEGER NOT NULL,   -- unix ms
    end_ts      INTEGER NOT NULL,   -- unix ms
    duration_ms INTEGER NOT NULL,
    size_bytes  INTEGER NOT NULL,
    has_motion  INTEGER NOT NULL DEFAULT 0,
    locked      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_segments_cam_start ON segments(camera_id, start_ts);

CREATE TABLE events (
    id            TEXT PRIMARY KEY,
    camera_id     TEXT NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    type          TEXT NOT NULL DEFAULT 'motion',
    start_ts      INTEGER NOT NULL,  -- unix ms
    end_ts        INTEGER,           -- unix ms, null while ongoing
    thumb_path    TEXT NOT NULL DEFAULT '',
    snapshot_path TEXT NOT NULL DEFAULT '',
    score         REAL NOT NULL DEFAULT 0,
    reviewed      INTEGER NOT NULL DEFAULT 0,
    saved         INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_events_cam_start ON events(camera_id, start_ts);

CREATE TABLE schedules (
    id        TEXT PRIMARY KEY,
    camera_id TEXT NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    weekday   INTEGER NOT NULL,  -- 0=Sunday .. 6=Saturday
    start_min INTEGER NOT NULL,  -- minutes from midnight
    end_min   INTEGER NOT NULL,
    mode      TEXT NOT NULL DEFAULT 'continuous'
);
CREATE INDEX idx_schedules_cam ON schedules(camera_id);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE audit_log (
    id      TEXT PRIMARY KEY,
    user_id TEXT,
    action  TEXT NOT NULL,
    target  TEXT NOT NULL DEFAULT '',
    ts      INTEGER NOT NULL
);
CREATE INDEX idx_audit_ts ON audit_log(ts);
