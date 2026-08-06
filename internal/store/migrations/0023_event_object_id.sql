-- 0023: give detections a stable id so a single object is addressable
-- (/events/{id}/detections/{detId}). event_objects previously had no id column.
-- Additive: ADD COLUMN is metadata-only; the backfill seeds existing rows from
-- their rowid; the UNIQUE index enforces distinctness. New rows get an
-- app-assigned id (a counter seeded at store open from MAX(id)).
ALTER TABLE event_objects ADD COLUMN id INTEGER;
UPDATE event_objects SET id = rowid;
CREATE UNIQUE INDEX IF NOT EXISTS idx_event_objects_id ON event_objects(id);
