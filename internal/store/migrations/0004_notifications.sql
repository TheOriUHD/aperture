-- Notifications: delivery channels, rules, web-push subscriptions, dedup log.

CREATE TABLE IF NOT EXISTS notify_channels (
    id         TEXT    PRIMARY KEY,
    user_id    TEXT    NOT NULL DEFAULT '',
    type       TEXT    NOT NULL,            -- webpush|ntfy|telegram|discord|webhook|email
    name       TEXT    NOT NULL DEFAULT '',
    config     TEXT    NOT NULL DEFAULT '{}',
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_channels_user ON notify_channels(user_id);

CREATE TABLE IF NOT EXISTS notify_rules (
    id            TEXT    PRIMARY KEY,
    user_id       TEXT    NOT NULL DEFAULT '',
    name          TEXT    NOT NULL DEFAULT '',
    enabled       INTEGER NOT NULL DEFAULT 1,
    cameras       TEXT    NOT NULL DEFAULT '',   -- JSON array; empty = all allowed
    event_types   TEXT    NOT NULL DEFAULT '',   -- JSON array; empty = all
    channels      TEXT    NOT NULL DEFAULT '',   -- JSON array of channel ids
    quiet_start   INTEGER NOT NULL DEFAULT -1,   -- minutes from midnight; -1 = disabled
    quiet_end     INTEGER NOT NULL DEFAULT -1,
    cooldown_sec  INTEGER NOT NULL DEFAULT 120,
    created_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_rules_user ON notify_rules(user_id);

CREATE TABLE IF NOT EXISTS push_subscriptions (
    id         TEXT    PRIMARY KEY,
    user_id    TEXT    NOT NULL,
    endpoint   TEXT    NOT NULL,
    p256dh     TEXT    NOT NULL,
    auth       TEXT    NOT NULL,
    ua         TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_push_user ON push_subscriptions(user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_push_endpoint ON push_subscriptions(endpoint);

CREATE TABLE IF NOT EXISTS notify_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id    TEXT    NOT NULL DEFAULT '',
    event_key  TEXT    NOT NULL DEFAULT '',
    ts         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notifylog_rule ON notify_log(rule_id, ts);
