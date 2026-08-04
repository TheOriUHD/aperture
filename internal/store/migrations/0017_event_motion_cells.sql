-- Grid Search: store a per-event coarse motion map (JSON array of active cell
-- indices in a 32×18 grid) so events can be filtered by where motion occurred.
-- Empty for events recorded before this, and for non-motion (e.g. audio) events.

ALTER TABLE events ADD COLUMN motion_cells TEXT NOT NULL DEFAULT '';
