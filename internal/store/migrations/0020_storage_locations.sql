-- Multi-location recording storage: named destinations (local path, SMB, SFTP)
-- footage can be written to. The built-in "default" location (id "") is virtual —
-- it maps to the existing recordings dir + storage_cap_bytes and is never stored
-- here, so existing footage and the current reserve wizard keep working unchanged.
--
-- config_json holds NON-secret, type-specific fields. secret_enc holds the
-- AES-GCM-encrypted share password/key (base64(nonce||ciphertext)); "" = none.
-- allocation_bytes: local = hard reserve, network = soft cap (0 = unset/unlimited).

CREATE TABLE IF NOT EXISTS storage_locations (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    type             TEXT NOT NULL DEFAULT 'local', -- local | smb | sftp
    config_json      TEXT NOT NULL DEFAULT '{}',
    secret_enc       TEXT NOT NULL DEFAULT '',
    allocation_bytes INTEGER NOT NULL DEFAULT 0,
    enabled          INTEGER NOT NULL DEFAULT 1,
    sort_order       INTEGER NOT NULL DEFAULT 0,
    created_at       INTEGER NOT NULL DEFAULT 0,
    updated_at       INTEGER NOT NULL DEFAULT 0
);

-- Which location a camera records to ("" = default), and which location a segment
-- physically lives on ("" = default/legacy root) so serve + delete route correctly.
ALTER TABLE cameras  ADD COLUMN location_id TEXT NOT NULL DEFAULT '';
ALTER TABLE segments ADD COLUMN location_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_segments_location ON segments(location_id);
