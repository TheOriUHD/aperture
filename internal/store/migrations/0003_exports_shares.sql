-- Clip export jobs + scoped share links.

CREATE TABLE IF NOT EXISTS export_jobs (
    id          TEXT    PRIMARY KEY,
    user_id     TEXT    NOT NULL DEFAULT '',
    camera_id   TEXT    NOT NULL,
    camera_name TEXT    NOT NULL DEFAULT '',
    from_ts     INTEGER NOT NULL,
    to_ts       INTEGER NOT NULL,
    burn_in     INTEGER NOT NULL DEFAULT 0,
    audio       INTEGER NOT NULL DEFAULT 1,
    status      TEXT    NOT NULL DEFAULT 'queued',
    progress    REAL    NOT NULL DEFAULT 0,
    output_path TEXT    NOT NULL DEFAULT '',
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    error       TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    finished_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_export_user ON export_jobs(user_id);

CREATE TABLE IF NOT EXISTS shares (
    id            TEXT    PRIMARY KEY,
    token_hash    TEXT    NOT NULL,
    kind          TEXT    NOT NULL,
    target        TEXT    NOT NULL,
    label         TEXT    NOT NULL DEFAULT '',
    expires_at    INTEGER NOT NULL DEFAULT 0,
    password_hash TEXT    NOT NULL DEFAULT '',
    max_views     INTEGER NOT NULL DEFAULT 0,
    views         INTEGER NOT NULL DEFAULT 0,
    created_by    TEXT    NOT NULL DEFAULT '',
    revoked       INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_shares_token ON shares(token_hash);
CREATE INDEX IF NOT EXISTS idx_shares_user ON shares(created_by);
