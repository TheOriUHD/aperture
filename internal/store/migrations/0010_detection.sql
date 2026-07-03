-- Event-gated object detection (opt-in). Motion opens an event, a detector
-- classifies a burst of frames, and the labels are attached here. Default build
-- ships no detector, so these stay empty and nothing changes.

ALTER TABLE events ADD COLUMN top_label TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN object_summary TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS event_objects (
    event_id   TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    label      TEXT NOT NULL,
    confidence REAL NOT NULL,
    bbox_x     REAL NOT NULL DEFAULT 0,  -- all normalized 0..1
    bbox_y     REAL NOT NULL DEFAULT 0,
    bbox_w     REAL NOT NULL DEFAULT 0,
    bbox_h     REAL NOT NULL DEFAULT 0,
    ts         INTEGER NOT NULL DEFAULT 0 -- frame time within the event (unix ms)
);
CREATE INDEX IF NOT EXISTS idx_event_objects_event ON event_objects(event_id);
