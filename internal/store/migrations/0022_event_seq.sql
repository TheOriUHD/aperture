-- 0022: monotonic event sequence — the stable cursor for /events and the SSE
-- Last-Event-ID. Additive: ADD COLUMN is metadata-only (no table rewrite); the
-- one-time backfill assigns seq in (start_ts, id) order; the UNIQUE index
-- enforces distinctness. New events get their seq from an in-process counter
-- (seeded at store open from MAX(seq)). seq is strictly increasing, not gapless.
ALTER TABLE events ADD COLUMN seq INTEGER;
UPDATE events SET seq = (
    SELECT COUNT(*) FROM events e2
    WHERE e2.start_ts < events.start_ts
       OR (e2.start_ts = events.start_ts AND e2.id <= events.id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_events_seq ON events(seq);
