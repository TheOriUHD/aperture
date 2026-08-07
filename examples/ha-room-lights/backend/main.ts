// Home Assistant Room Lights — backend.
//
// Reacts to the Aperture change feed and turns on a room's Home Assistant lights
// when a new event fires on one of the granted cameras. It uses only the
// @aperture/sdk surface — no raw filesystem, database, or subprocess access.
//
// The bare specifier below resolves via the supervisor's generated import map to
// the local, read-only SDK module, so it works under --deny-import (no remote
// code is ever fetched).

import { aperture } from "@aperture/sdk";

// The cameras this extension was granted (manifest: permissions.cameras).
const ROOMS = ["hallway", "front-door"];

// Home Assistant target. The manifest declared `network` access to
// homeassistant.local:8123 only — any other host throws NotCapable.
const HA_BASE = "https://homeassistant.local:8123";

// A Home Assistant long-lived token. Backends run headless (no prompt) and cannot
// read arbitrary host environment variables, so set your token here before
// installing, or serve the bundle from your own trusted source.
const HA_TOKEN = "REPLACE_WITH_YOUR_HOME_ASSISTANT_TOKEN";

// Subscribe to every new event. The server only delivers events this extension's
// token (cameras:read, events:read on hallway + front-door) is allowed to see, so
// the ROOMS guard below is belt-and-suspenders.
aperture.on("event.created", async (ev) => {
  if (!ev.camera || !ROOMS.includes(ev.camera)) return;
  try {
    await aperture.http.fetch(`${HA_BASE}/api/services/light/turn_on`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${HA_TOKEN}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ entity_id: `light.${ev.camera}` }),
    });
  } catch (err) {
    // A disallowed host throws NotCapable; a transient network failure throws too.
    // Don't let one failed call tear down the subscription.
    console.error("ha-room-lights: turn_on failed for", ev.camera, err);
  }
});
