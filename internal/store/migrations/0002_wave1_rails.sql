-- Wave 1 rails: tamper-evident audit log + per-user preferences.
-- 0001 shipped an unused placeholder audit_log; replace it with the
-- hash-chained schema (it was never written to, so dropping is safe).

DROP TABLE IF EXISTS audit_log;

CREATE TABLE audit_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts          INTEGER NOT NULL,
    actor_id    TEXT    NOT NULL DEFAULT '',
    actor_email TEXT    NOT NULL DEFAULT '',
    action      TEXT    NOT NULL,
    target      TEXT    NOT NULL DEFAULT '',
    detail      TEXT    NOT NULL DEFAULT '',
    ip          TEXT    NOT NULL DEFAULT '',
    ua          TEXT    NOT NULL DEFAULT '',
    prev_hash   TEXT    NOT NULL DEFAULT '',
    hash        TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit_log(ts);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action);
CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_log(actor_email);

CREATE TABLE IF NOT EXISTS user_preferences (
    user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    prefs      TEXT NOT NULL DEFAULT '{}',
    updated_at INTEGER NOT NULL DEFAULT 0
);
