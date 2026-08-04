-- Optional Home Assistant temperature-sensor entity assigned to a camera; its
-- live reading is shown in the camera's on-screen conditions overlay instead of
-- outdoor weather. Empty = none (fall back to weather).
ALTER TABLE cameras ADD COLUMN temp_sensor TEXT NOT NULL DEFAULT '';
