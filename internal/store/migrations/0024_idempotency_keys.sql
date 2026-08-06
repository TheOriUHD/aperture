-- 0024: idempotency keys for expensive creates (exports, shares). A repeated
-- request carrying the same Idempotency-Key replays the stored response instead
-- of doing the work twice. Additive: a brand-new table.
CREATE TABLE IF NOT EXISTS idempotency_keys (
    key         TEXT PRIMARY KEY,
    method      TEXT NOT NULL,
    path_hash   TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    response    TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL
);
