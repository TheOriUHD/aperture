-- Mirrored recording destinations: a camera may write to SEVERAL storage
-- locations at once, each with its own retention.
--
-- Model: `cameras.location_id` stays the PRIMARY destination — the one ffmpeg
-- writes to (unchanged, zero-migration back-compat). camera_destinations holds
-- the full membership set (primary included, for uniformity); every membership
-- beyond the primary is a MIRROR, filled by copying completed segments.
--
-- A segment row remains the canonical copy of a recording. segment_copies holds
-- the extra physical copies; retention treats each copy independently, and when
-- the canonical copy dies while a mirror survives, the mirror is PROMOTED into
-- the segment row (playback never loses footage a destination still keeps).

CREATE TABLE camera_destinations (
    camera_id      TEXT NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    location_id    TEXT NOT NULL,              -- '' = default location
    retention_days INTEGER NOT NULL DEFAULT 0, -- 0 = inherit the camera's retentionDays
    PRIMARY KEY (camera_id, location_id)
);

CREATE TABLE segment_copies (
    segment_id  TEXT NOT NULL REFERENCES segments(id) ON DELETE CASCADE,
    location_id TEXT NOT NULL,
    path        TEXT NOT NULL,                 -- absolute (local) or backend-relative (network)
    size_bytes  INTEGER NOT NULL,
    created_at  INTEGER NOT NULL,              -- unix ms
    PRIMARY KEY (segment_id, location_id)
);
CREATE INDEX idx_copies_location ON segment_copies(location_id);

-- Seed: every camera's current single destination becomes its sole membership.
INSERT INTO camera_destinations (camera_id, location_id, retention_days)
    SELECT id, location_id, 0 FROM cameras;
