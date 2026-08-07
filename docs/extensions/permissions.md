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
| `cameras: [ids]`                         | per-camera grants, composed as **owner ∩ grants**                             | "…only for: \<camera names>"                                |
| `network: [hosts]`                       | **backend only** — the Deno `--allow-net` hosts (plus Aperture's own API host) | "Connect over the internet to \<hosts> — it can send your data there" |
| `storage: true`                          | a private, quota'd key-value namespace                                         | "Keep its own settings in a private folder"                 |

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

## Camera grants

`permissions.cameras` lists the camera ids the extension may touch. Effective reach
is always **the owner's cameras ∩ the grant list**:

- **Omitted or empty** → all of the owner's cameras (for any camera-touching scope).
- **Listed** → only those cameras, and only the ones the owner can actually see.

For a **UI panel** there is a second intersection: the panel is also capped by
**the viewing user's** own camera ACL. So `aperture.cameras.list()` in a panel
returns *this viewer ∩ this extension*, which can be fewer cameras than the
manifest lists — and fewer for a restricted viewer than for an admin. That is
correct behavior; see [security.md](security.md#per-viewer-authority).

## Network is backend-only

`permissions.network` is a list of absolute `http(s)` URLs. Only a **backend** may
declare it — a UI-only panel cannot make outbound requests (its sandbox forbids all
network of its own). At runtime the declared hosts become the backend's Deno
`--allow-net` allowlist (Aperture injects its own API host too, so the SDK can
reach it); every other host throws `NotCapable`.

This is the **only** way a backend reaches the outside world, and **it can send
your data there.** Grant `network` only to extensions and hosts you trust.

## Storage

`storage: true` reserves a private, quota'd key-value namespace for the extension,
isolated from other extensions and from Aperture's global settings. It grants no
API scope and no shared access — just a small private place for the extension's own
configuration. Reach it through `aperture.storage.get/set/delete/keys` on **both**
transports (see the SDK reference); the namespace is destroyed when the extension is
uninstalled.

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
