-- Automation engine (§14.1): when-this-then-that rules local to Aperture.
-- trigger/conditions/actions are JSON blobs so the rule shape can grow without
-- further migrations. Actions dispatch to existing subsystems (notify, webhooks,
-- MQTT, the recorder).

CREATE TABLE IF NOT EXISTS automations (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    trigger     TEXT NOT NULL DEFAULT '{}',   -- {eventTypes:[], objects:[], cameras:[]}
    conditions  TEXT NOT NULL DEFAULT '{}',   -- {timeOfDay, onlyPrecip}
    actions     TEXT NOT NULL DEFAULT '[]',   -- [{type, ...}]
    cooldown_ms INTEGER NOT NULL DEFAULT 0,
    last_fired  INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL
);
