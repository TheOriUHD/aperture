-- Open-platform integrations: scoped API tokens (Bearer auth for automation,
-- bound to a user so they inherit that user's role + camera ACLs) and outbound
-- event webhooks (POST signed JSON to any URL, with a delivery log).

CREATE TABLE IF NOT EXISTS api_tokens (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,          -- sha256(token), the plaintext is shown once
    prefix     TEXT NOT NULL DEFAULT '',      -- leading chars for display
    created_at INTEGER NOT NULL,
    last_used  INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL DEFAULT 0     -- 0 = never expires
);
CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);

CREATE TABLE IF NOT EXISTS webhooks (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    url        TEXT NOT NULL,
    events     TEXT NOT NULL DEFAULT '',      -- CSV of event types; empty = all
    secret     TEXT NOT NULL DEFAULT '',      -- HMAC-SHA256 signing key
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id         TEXT PRIMARY KEY,
    webhook_id TEXT NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event      TEXT NOT NULL,
    status     TEXT NOT NULL,                 -- ok | failed
    code       INTEGER NOT NULL DEFAULT 0,    -- HTTP status (0 = no response)
    detail     TEXT NOT NULL DEFAULT '',
    ts         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_wh ON webhook_deliveries(webhook_id, ts);
