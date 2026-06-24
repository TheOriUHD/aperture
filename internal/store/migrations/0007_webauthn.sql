-- Passkeys (WebAuthn credentials).

CREATE TABLE IF NOT EXISTS webauthn_credentials (
  id         TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL,
  name       TEXT NOT NULL DEFAULT '',
  cred_id    TEXT NOT NULL,   -- base64url(credential.ID), for lookup + uniqueness
  cred_json  TEXT NOT NULL,   -- full webauthn.Credential JSON
  created_at INTEGER NOT NULL,
  last_used  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_webauthn_user ON webauthn_credentials(user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_webauthn_credid ON webauthn_credentials(cred_id);
