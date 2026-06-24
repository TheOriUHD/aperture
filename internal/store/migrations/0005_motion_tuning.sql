-- Per-camera motion tuning (promote the global pre/post-roll + cooldown
-- constants to per-camera, add a minimum-duration blip filter).

ALTER TABLE cameras ADD COLUMN preroll_ms INTEGER NOT NULL DEFAULT 5000;
ALTER TABLE cameras ADD COLUMN postroll_ms INTEGER NOT NULL DEFAULT 5000;
ALTER TABLE cameras ADD COLUMN cooldown_ms INTEGER NOT NULL DEFAULT 5000;
ALTER TABLE cameras ADD COLUMN min_duration_ms INTEGER NOT NULL DEFAULT 0;
