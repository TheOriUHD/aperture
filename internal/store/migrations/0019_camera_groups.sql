-- Camera groups: admin-defined named sets of cameras, used by the mobile
-- dashboard's group selector. Membership is a JSON array of camera ids (a camera
-- may belong to several groups). The built-in "All Cameras" group is virtual and
-- not stored here.

CREATE TABLE IF NOT EXISTS camera_groups (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    cameras_json TEXT NOT NULL DEFAULT '[]',
    sort_order   INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL DEFAULT 0,
    updated_at   INTEGER NOT NULL DEFAULT 0
);
