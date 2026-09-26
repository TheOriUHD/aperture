# Changelog

Aperture shipped its first three months untagged: 212 commits from the first
Phase 0 commit (2026-06-23) to a native systemd deployment recording four cameras
in production, with every build stamping `0.0.0-dev`.

The versions below were reconstructed from the git history and tagged at the
commit where each milestone landed, so `git describe` — and therefore every
build — names a real release. They are a reading of the history rather than
releases that were cut at the time.

`1.0.0` is the 2026-08-20 cutover: the point where Aperture stopped being a
Docker stack on a shared VM and became a native systemd deployment on its own
hardware, recording production cameras behind a frozen `/api/v1`.

## 1.2.0 — 2026-09-26
- A disabled camera no longer appears on the dashboard, in playback, in
  multi-view or in export pickers, instead of showing a placeholder tile labelled
  "Disabled". Camera management still lists every camera, which is where a
  disabled one is switched back on.
- With cameras configured but all of them off, the dashboard says so rather than
  "No cameras yet".
- The camera editor's Enabled toggle moved to the top, above the settings it
  governs.
- The build derives its version from git tags; `0.0.0-dev` is gone.

## 1.1.0 — 2026-09-10 (`b88982e`)
- Audio restored under ffmpeg 7, which announced an AAC track and then wrote no
  packets to it; segments recorded while it was broken are repaired as they are
  served.
- Eufy account bridge, configured from Settings instead of a shell.
- Player extension slots (toolbar and on-screen overlay) sharing one OSD opacity,
  with Tapo and Eufy control examples.
- Explicit per-camera Reolink marking for spotlight and IR, replacing a live probe
  that made the controls vanish whenever the camera was unreachable.

## 1.0.0 — 2026-08-20 (`f08c9c7`) — production deployment
- Native systemd units on dedicated hardware, replacing Docker Compose. The
  cutover cost 28 seconds of recording.
- Pinned runtime vendoring, a hardening preflight, and a retention gate that
  predicts the janitor's first pass before the daemon starts.
- mDNS/DNS-SD advertisement (`aperture.local`).

## 0.12.0 — 2026-08-19 (`4e64d07`)
- Mirrored storage destinations: record to several locations at once, with
  per-destination retention.
- Camera controls from the live view: ONVIF picture settings, Reolink spotlight
  and IR.
- ONVIF and HomeKit became UI toggles; the playback rail became a flat
  three-state timeline.
- Home Assistant live card: managed live key and camera slugs.

## 0.11.0 — 2026-08-07 (`2691245`) — extension platform
- Deno backend extensions under a non-root uid with deny-by-default permissions.
- Sandboxed UI panels with a confused-deputy-safe broker (extension ∩ caller).
- Manager UI: install, consent, pins, curated store, updates with revert, editor.
- Private per-extension storage, settings with write-only secrets, and
  host-mediated egress.

## 0.10.0 — 2026-08-06 (`6fe03da`) — frozen /api/v1
- `/api/v1` alongside the legacy `/api` alias, with capability discovery.
- Central declarative authorization that refuses to boot on an undeclared route.
- Scoped API tokens, ETag/If-Match, idempotency keys, keyset pagination, segment
  leases, an SSE change feed, and OpenAPI generated from the route registry.

## 0.9.0 — 2026-08-02 (`378c73e`)
- Storage reframed around Aperture's own allocation rather than the whole disk.
- Multi-camera export: zip and composite multi-view.
- Playback, shell and mobile sweep, including iOS fullscreen and safe areas.

## 0.8.0 — 2026-07-03 (`a1378c9`)
- Scoped API tokens, outbound webhooks, OpenAPI.
- Automation engine, inbound Home Assistant control, ONVIF and HomeKit.
- Opt-in, event-gated object detection.

## 0.7.0 — 2026-06-30 (`4abfee6`)
- System health dashboard; resilience and operability passes.
- Storage redesign: global cap, configurable path, tabbed settings.
- On-device notifications without an external push service.

## 0.6.0 — 2026-06-24 (`a2212c2`)
- Clip export and scoped share links.
- Notifications engine and channels, PWA, web push.
- Motion quality: per-camera roll, cooldown and sensitivity, paintable zones.
- Audit log, per-user preferences, i18n (DE/EN), first-login flow, TOTP 2FA and
  passkeys.

## 0.5.0 — 2026-06-24 (`476cd99`)
- Home Assistant MQTT integration (outbound).
- `nvrctl` full-screen admin console; OIDC/SSO login.
- Mobile playback layout and responsive bottom nav.

## 0.4.0 — 2026-06-23 (`7aa3de0`)
- Record modes honoured; multi-user roles with per-camera access.
- Weekly schedule editor; admin storage management.

## 0.3.0 — 2026-06-23 (`c9e547e`)
- Motion detection and events.

## 0.2.0 — 2026-06-23 (`4b771f7`)
- Continuous recorder, retention janitor, recordings API.
- UniFi-style playback with a fixed-playhead scrub timeline.

## 0.1.0 — 2026-06-23 (`dfa25ee`)
- Backend foundation: config, SQLite store, auth, go2rtc stream manager, API.
- React UI: design system, app shell, live tiles.
