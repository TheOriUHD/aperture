-- Where each recognised object was, a few times a second, while its event ran:
-- playback draws boxes that follow the object instead of one still box.
-- obj is the object's id from the live tracker (unique per camera for the life
-- of the process; with event_id it identifies one object's path).
CREATE TABLE IF NOT EXISTS object_tracks (
    event_id   TEXT    NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    camera_id  TEXT    NOT NULL,
    obj        INTEGER NOT NULL,
    label      TEXT    NOT NULL,
    confidence REAL    NOT NULL,
    x          REAL    NOT NULL, -- all normalized 0..1
    y          REAL    NOT NULL,
    w          REAL    NOT NULL,
    h          REAL    NOT NULL,
    ts         INTEGER NOT NULL  -- unix ms
);
CREATE INDEX IF NOT EXISTS idx_object_tracks_cam_ts ON object_tracks(camera_id, ts);
CREATE INDEX IF NOT EXISTS idx_object_tracks_event ON object_tracks(event_id);
