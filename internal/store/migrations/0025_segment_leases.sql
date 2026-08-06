-- 0025: segment leases (T2.8) — a real, expiring lease that protects footage
-- from retention while a long-running consumer (an export, a playback session,
-- an extension) needs it. Replaces the interim in-memory active-export snapshot
-- and the inert segments.locked flag: the janitor deletes a segment only when no
-- unexpired lease overlaps it. Additive: a brand-new table, no rewrite of the
-- large segments table (a range lease, not a per-segment flag — see plan C-8/C4).
CREATE TABLE IF NOT EXISTS segment_leases (
    id         TEXT PRIMARY KEY,
    camera_id  TEXT NOT NULL,
    from_ts    INTEGER NOT NULL, -- unix ms, inclusive window start
    to_ts      INTEGER NOT NULL, -- unix ms, exclusive window end
    holder     TEXT NOT NULL,    -- who holds it (e.g. "export:<jobid>", "token:<uid>")
    expires_at INTEGER NOT NULL  -- unix ms; a lease past this is ignored and swept
);
CREATE INDEX IF NOT EXISTS idx_segment_leases_lookup ON segment_leases (camera_id, expires_at);
