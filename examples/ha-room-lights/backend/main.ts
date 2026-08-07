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

// This extension is granted your cameras (manifest: permissions.data.cameras +
// permissions.events). The change feed only delivers events for cameras this
// extension AND your token may see, so no extra id filtering is required here —
// scope it down by listing specific ids in permissions.cameras if you prefer.

// Home Assistant target. The manifest declared `network` access to
// homeassistant.local:8123 only — any other host throws NotCapable.
const HA_BASE = "https://homeassistant.local:8123";

// The Home Assistant long-lived token lives in this extension's own PRIVATE storage
// (the `storage` permission) — set it once from the panel (window.aperture.storage)
// or a setup step. It is namespaced to this extension: no other extension can read
// it, and it is destroyed when this extension is uninstalled.

// Subscribe to every new event the extension's token is allowed to see (its camera
// grant ∩ the owner's cameras), and turn on that camera's room light.
aperture.on("event.created", async (ev) => {
  if (!ev.camera) return;
  const haToken = await aperture.storage.get("ha_token");
  if (!haToken) {
    console.error("ha-room-lights: no ha_token in storage yet — set it from the panel");
    return;
  }
  try {
    await aperture.http.fetch(`${HA_BASE}/api/services/light/turn_on`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${haToken}`,
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
