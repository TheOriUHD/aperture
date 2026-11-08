-- Per-camera recording quality, independent of live view: the highest resolution
-- (height in pixels), the highest framerate and the video bitrate (kbit/s) to record
-- at. 0 means "as the camera sends it", which keeps the stream-copy recording path.
ALTER TABLE cameras ADD COLUMN record_height INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cameras ADD COLUMN record_fps INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cameras ADD COLUMN record_kbps INTEGER NOT NULL DEFAULT 0;
