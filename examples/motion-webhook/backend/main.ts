// Motion Webhook — backend (headless).
//
// The canonical BACKEND example: an extension with no UI that reacts to the Aperture
// change feed and POSTs to a webhook when motion is detected on one of the granted
// cameras — nobody has to be watching. This is the original motivating case: acting on
// a detection unattended.
//
// It uses only the @aperture/sdk surface — no raw filesystem, database, or subprocess
// access. The bare specifier below resolves via the supervisor's generated import map
// to the local, read-only SDK module, so it runs under --deny-import (no remote code is
// ever fetched).

import { aperture } from "@aperture/sdk";

// Subscribe to every new event the extension's token is allowed to see. The change
// feed is already scoped to the granted cameras ∩ the owner's cameras, so no extra id
// filtering is needed here — narrow permissions.cameras if you want fewer.
//
// Config is read INSIDE the handler, not at module load. A top-level `await` against the
// API would make startup depend on the server being reachable; reading per event keeps
// startup instant and always sees the current settings (the extension also restarts when
// settings change).
aperture.on("event.created", async (ev) => {
  if (!ev.camera) return;

  // A backend reads its settings through storage (its private namespace) — the panel-only
  // `settings` object is not needed here, and unlike a panel the backend reads secrets in
  // the clear (server-side).
  const url = await aperture.storage.get("webhook_url");
  if (!url) return; // not configured yet — nothing to call

  const headers: Record<string, string> = { "Content-Type": "application/json" };
  const token = await aperture.storage.get("auth_token"); // a SECRET setting; readable here, never by a panel
  if (token) headers["Authorization"] = `Bearer ${token}`;

  try {
    // permissions.network allows exactly hooks.example.com — any other host throws
    // NotCapable, so a compromised webhook_url can't exfiltrate elsewhere.
    await aperture.http.fetch(url, {
      method: "POST",
      headers,
      body: JSON.stringify({ text: `Motion detected on ${ev.camera}`, camera: ev.camera, at: ev.ts }),
    });
  } catch (err) {
    // A disallowed host or a transient failure throws; don't let one failed call tear
    // down the subscription.
    console.error("motion-webhook: post failed for", ev.camera, err);
  }
});

// Startup marker: proves the module loaded and the SDK import resolved. The end-to-end
// test asserts this line, so it is also the "backend extensions actually run" signal.
console.log("motion-webhook ready");
