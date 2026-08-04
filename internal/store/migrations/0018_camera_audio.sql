-- Audio detection: a per-camera toggle + loudness threshold (0..1, 0 = use the
-- built-in default). When on, a per-camera audio detector emits "audio" events
-- when the decoded audio level crosses the threshold.

ALTER TABLE cameras ADD COLUMN audio_detection INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cameras ADD COLUMN audio_threshold REAL NOT NULL DEFAULT 0;
