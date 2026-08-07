# Permissions

An extension's `permissions` block is the **complete** list of what it may do —
nothing outside it is grantable. The friendly permission names map 1:1 onto
Aperture's existing API scopes and per-camera grants (there is no separate
authorization system), plus two non-API concerns: `network` and `storage`.

Permissions are **deny-by-default**: an absent or empty block grants nothing.

## Catalogue

| Manifest permission                      | What it grants                                                                 | Consent wording                                              |
| ---------------------------------------- | ------------------------------------------------------------------------------ | ----------------------------------------------------------- |
| `data.<domain>: "read"` / `"write"`      | the `<domain>:read` / `<domain>:write` scope on the extension's token; `write` implies `read` | "Read your \<domain>" / "Read and change your \<domain>"    |
| `data.media: "live"`                     | the `media:live` scope (watch live streams — bandwidth/privacy, off by default) | "Watch your live camera streams"                            |
| `alarm: "arm"`                           | the `alarm:arm` scope                                                          | "Arm/disarm the alarm"                                      |
| `system: "read"`                         | the `system:read` scope (health/metrics/status)                               | "Read system health"                                        |
| `cameras: "all"` \| `[ids]`              | per-camera grants (opt-in — see below), composed as **owner ∩ grants**        | one honest camera line + a picker at consent                |
| `network: [hosts]`                       | the Deno `--allow-net` hosts for a backend **and** the host-mediated `net.fetch` hosts for a panel (plus Aperture's own API host) | "Connect over the internet to \<hosts> — it can send your data there" |
| `storage: true`                          | a private, quota'd key-value namespace (also implied by `settings`)           | "Keep its own settings in a private folder"                 |

The consent screen also always states, verbatim, what the extension **cannot** do:
"It cannot read your recordings files, your database, or your passwords."

### data domains

`data` maps a domain to a grant level. The grantable domains and levels are:

| Domain       | `read` | `write` | `live` |
| ------------ | :----: | :-----: | :----: |
| `cameras`    |   ✓    |    ✓    |        |
| `events`     |   ✓    |    ✓    |        |
| `coverage`   |   ✓    |         |        |
| `segments`   |   ✓    |         |        |
| `media`      |   ✓    |    ✓    |   ✓    |
| `storage`    |   ✓    |    ✓    |        |
| `schedules`  |   ✓    |    ✓    |        |
| `zones`      |   ✓    |    ✓    |        |
| `detection`  |   ✓    |    ✓    |        |
| `site`       |   ✓    |    ✓    |        |
| `automation` |   ✓    |    ✓    |        |

`write` implies `read`. A domain or level not in this table is rejected at install.

> Note: `data.storage` (the `storage:read`/`storage:write` API scope for Aperture's
> own *storage-locations* resource) is a different thing from the top-level
> `storage: true` permission (the extension's own private key-value store). They
> are unrelated — you may want one, the other, both, or neither.

## Camera access is opt-in

Camera access is the primary privacy control, so it is **explicit**. Most extensions
never touch a camera feed — a panel that adds a control to the camera view is just UI
sitting there — and those get **no camera ceremony at all**: no picker, no camera line
at consent, nothing.

`permissions.cameras` is how an extension opts in:

- **Absent** → **no camera access.** The extension gets no camera data and no
  camera-scoped UI. (An extension that declares a camera-touching `data` scope —
  `cameras`, `events`, `coverage`, `segments`, `media` — but omits `cameras` is
  rejected at install: say what you want.)
- **`"all"`** → every camera, **including any added later** (future-inclusive). This
  is a deliberate, explicit choice — distinct from ticking every current camera.
- **`[ids]`** → exactly those cameras, as a *ceiling*.

The request is a **default, not a setting**: at install the admin sees one honest line
(scaled to what the extension can see — "watch your cameras' live video", "see your
cameras and their events", or, for a control-only panel, "add its controls to your
cameras' views") and a picker, and **narrows the grant**. For an `"all"` request the
picker offers a clear choice between *all cameras (incl. future)* and *only the cameras
I choose*. The grant is **editable afterwards from the extension's row** (re-minting the
token; changing camera scope is the admin's decision, not a new permission, so it never
re-prompts consent).

**The grant gates UI presence, not just data.** A camera-detail panel granted only
*Hallway* renders on Hallway and nowhere else; one decision controls both what the
extension can read and where it appears. An empty grant (an `"all"` request, or a
pure-UI panel that requested no cameras) renders everywhere. None of this appears in
day-to-day use — no scope badges, no chips on the camera view; the decision happens at
install or in the extension's settings, and after that the UI is just the UI.

For a **UI panel** there is a second intersection: the panel is also capped by **the
viewing user's** own camera ACL. So `aperture.cameras.list()` in a panel returns *this
viewer ∩ this extension*, which can be fewer cameras than the manifest lists — and fewer
for a restricted viewer than for an admin. That is correct; see
[security.md](security.md#per-viewer-authority).

## Network

`permissions.network` is a list of absolute `http(s)` URLs — the hosts an extension may
reach. A **backend** reaches them via Deno `--allow-net`; a **UI panel** reaches them via
host-mediated `aperture.net.fetch`, where the host performs the request server-side,
bounded to these same declared hosts (Aperture injects its own API host too, so the SDK
can reach it). Every other host is refused.

This is the only way an extension reaches the outside world, and **it can send your data
there.** For a panel using `net.fetch`, anyone who can use that panel can trigger a
request to the declared host — with any secret the extension injects (see below). Grant
`network` only to extensions and hosts you trust.

## Storage and settings

`storage: true` reserves a private, quota'd key-value namespace for the extension,
isolated from other extensions and from Aperture's global settings. Reach it through
`aperture.storage.get/set/delete/keys` on **both** transports; the namespace is
destroyed on uninstall.

Declaring a **`settings`** schema (see [settings.md](settings.md)) **implies** `storage`
— settings live in that same namespace — so you never write both. A field typed
`secret` is **write-only**: its value is never returned to any client (not even an
admin), only whether one is set. A panel authenticates to a declared host with a
`{{secret:key}}` placeholder in `net.fetch`, which the host expands server-side, so the
plaintext never enters the browser.

## Never grantable

No permission — at any level — can reach the dangerous, **session-only**
operations. These have **no scope at all**, so no extension token can ever hold
them, regardless of what a manifest asks for:

- creating, changing, or deleting **users** or **API tokens**
- **backup** / **restore**
- **restart**
- **reindex**
- **purge**

The manifest schema rejects any attempt to request these, and the consent screen
states them as things the extension **cannot** do.
