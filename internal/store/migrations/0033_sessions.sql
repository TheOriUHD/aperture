-- Server-side browser sessions and trusted devices.
--
-- Sessions used to be self-contained signed cookies with a fixed 7-day life: the
-- web app logged everyone out weekly, however often they used it, and nothing
-- could end a session early (logout only dropped the cookie; a password change or
-- a disabled account left every issued cookie working until it expired). Storing
-- sessions lets them slide forward with use, be listed per device and revoked.
--
-- The cookie carries a random token; only its SHA-256 is stored, so a copy of the
-- database cannot be replayed as a login.
CREATE TABLE IF NOT EXISTS sessions (
  id             TEXT PRIMARY KEY,
  user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash     TEXT NOT NULL UNIQUE,
  device_id      TEXT NOT NULL DEFAULT '', -- trusted_devices.id this browser holds, if any
  created_at     INTEGER NOT NULL,          -- unix seconds
  last_seen_at   INTEGER NOT NULL,
  expires_at     INTEGER NOT NULL,          -- slides forward with use (idle window)
  max_expires_at INTEGER NOT NULL,          -- absolute cap from sign-in
  ip             TEXT NOT NULL DEFAULT '',
  ua             TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

-- A browser that has completed a second factor (authenticator code, recovery code
-- or passkey). Signing in again with the password from it skips the code. Survives
-- logout and session expiry; that is its purpose.
CREATE TABLE IF NOT EXISTS trusted_devices (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash   TEXT NOT NULL UNIQUE,
  created_at   INTEGER NOT NULL,
  last_used_at INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,
  ip           TEXT NOT NULL DEFAULT '',
  ua           TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_trusted_devices_user ON trusted_devices(user_id);
