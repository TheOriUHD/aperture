-- Keep a camera out of Home Assistant and HomeKit. 0 (published) for every existing
-- camera, so nothing already in Home Assistant changes; a UniFi Protect camera is
-- added hidden, since Home Assistant usually has those through its own integration.
ALTER TABLE cameras ADD COLUMN ha_hidden INTEGER NOT NULL DEFAULT 0;
