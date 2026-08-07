# Manifest reference

Every extension bundle has a `manifest.json` at its root. It declares the
extension's identity, everything it is allowed to do, and where its code lives.
Aperture parses it **strictly** — unknown fields are rejected — so a typo in a
permission key fails loudly at install rather than silently granting the wrong
thing.

## Bundle layout

A bundle is a zip:

```
manifest.json        # required
ui/                  # optional — panel HTML + assets, served at /ext/{id}/ui/*
backend/             # optional — the Deno entry; never web-served
```

`manifest.json` must be **strict JSON** — no comments, no trailing commas, no
unknown fields. The annotated example below uses `//` comments for explanation
only; remove them in a real file.

## Worked example

```jsonc
{
  "manifest": 1,                       // manifest schema version (int; gates parsing)
  "id": "com.example.ha-room-lights",  // reverse-DNS, immutable identity, [a-z0-9.-]
  "name": "Home Assistant Room Lights",
  "version": "1.2.0",                  // semver; used for update comparison
  "author": "Jane Doe",
  "description": "Adds a camera-view panel and turns on the room's Home Assistant lights while you watch.",
  "icon": "ui/icon.png",               // optional: a PNG under ui/, <=256x256, shown in the store & manager
  "minApiVersion": "v1",               // refuse install if the server API is older
  "permissions": {                     // EVERYTHING an extension may do; deny-by-default
    "data": { "cameras": "read" },                     // maps to the cameras:read scope
    "cameras": "all",                                  // OPT-IN: "all" (incl. future) or [ids]; omit = no camera access
    "network": ["https://homeassistant.local:8123"]    // reachable hosts (backend + panel net.fetch)
    // storage is implied by `settings` below
  },
  "settings": [                        // the extension's own settings form; implies storage
    { "key": "ha_url",   "label": "Home Assistant URL", "type": "text",   "required": true, "default": "https://homeassistant.local:8123" },
    { "key": "ha_token", "label": "Access token",       "type": "secret", "required": true },
    { "key": "entity_template", "label": "Light entity", "type": "text",  "default": "light.{camera}" }
  ],
  "ui": {
    "panels": [
      { "slot": "camera-detail", "entry": "ui/panel.html", "title": "Room lights" }
    ]
  },
  "update": { "url": "https://example.com/ha-room-lights/manifest.json" }  // optional self-hosted update source
}
```

The canonical bundle in [`examples/ha-room-lights/`](../../examples/ha-room-lights/)
is this manifest as strict JSON, without the optional `icon` (it ships no icon file).
It is UI-only — the panel reads the current camera, its configured light, and toggles
it in Home Assistant using the `ha_token` **secret** via a `{{secret:ha_token}}`
placeholder (see [settings.md](settings.md) and [panels.md](panels.md)).

## Fields

### Top level

| Field           | Type   | Required    | Notes                                                                                     |
| --------------- | ------ | ----------- | ----------------------------------------------------------------------------------------- |
| `manifest`      | int    | **yes**     | Schema version. Must be `1`; a different value is rejected at parse.                       |
| `id`            | string | **yes**     | Reverse-DNS identifier (e.g. `com.example.thing`), lowercase `[a-z0-9]`, `.` and `-`. This is the extension's immutable identity. |
| `name`          | string | **yes**     | Display name.                                                                             |
| `version`       | string | **yes**     | Semver `x.y.z` (optional `-prerelease`). Update checks compare this.                       |
| `author`        | string | recommended | Author name (shown at install).                                                           |
| `description`   | string | recommended | One line describing what the extension does.                                              |
| `icon`          | string | no          | Bundle-relative path to a PNG **under `ui/`** (e.g. `ui/icon.png`), max 256×256. Used as the extension's icon in the store and manager. |
| `minApiVersion` | string | no          | Refuse install if the server's API is older. Currently `""` (any) or `"v1"`; `"v1"` recommended. |
| `permissions`   | object | no          | The complete list of what the extension may do — see [permissions.md](permissions.md).    |
| `ui`            | object | conditional | Panel declarations. Required if there is no `backend`.                                     |
| `backend`       | object | conditional | The Deno backend. Required if there is no `ui`.                                            |
| `update`        | object | no          | A self-hosted update source.                                                              |

An extension must declare **at least one** of `ui.panels` or `backend.entry`.

### permissions

Deny-by-default: an empty or absent `permissions` block grants nothing. Full
catalogue and consent wording in [permissions.md](permissions.md).

| Field                 | Type              | Notes                                                                              |
| --------------------- | ----------------- | ---------------------------------------------------------------------------------- |
| `permissions.data`    | object            | Map of `domain → "read"` / `"write"` (`media` also accepts `"live"`). See the domain table in [permissions.md](permissions.md#data-domains). |
| `permissions.alarm`   | string            | Only `"arm"`.                                                                       |
| `permissions.system`  | string            | Only `"read"`.                                                                      |
| `permissions.cameras` | `"all"` \| string[] | Camera access is **opt-in**: `"all"` (every camera, incl. future) or a list of ids (a ceiling); **omitted = no camera access**. Composed as **owner ∩ grants**. See [permissions.md](permissions.md#camera-access-is-opt-in). |
| `permissions.network` | string[]          | Absolute `http(s)` URLs — the hosts a backend (Deno `--allow-net`) or a panel (`net.fetch`) may reach. |
| `permissions.storage` | bool              | `true` reserves a private, quota'd key-value namespace. Also **implied by `settings`**. |
| `settings`            | object[]          | The extension's own settings schema (implies `storage`). See [settings.md](settings.md). |

### ui

| Field          | Type   | Required                | Notes                                               |
| -------------- | ------ | ----------------------- | --------------------------------------------------- |
| `ui.panels`    | array  | yes (if `ui` present)   | One entry per mounted panel.                        |
| `panel.slot`   | string | **yes**                 | One of the known slots (below).                     |
| `panel.entry`  | string | **yes**                 | Bundle-relative path to an `.html` file (no leading `/`, no `..`). |
| `panel.title`  | string | recommended             | Shown as the panel's heading.                       |

**Known slots** (Pass 1):

| Slot            | Where it renders                                        |
| --------------- | ------------------------------------------------------- |
| `camera-detail` | A panel on a camera's detail view (the motivating slot).|
| `settings`      | A section in Settings.                                  |
| `dashboard-card`| A card on the dashboard/overview.                       |
| `nav-panel`     | A full-page view reachable from the nav rail.           |

### backend

| Field                 | Type   | Required                  | Notes                                                    |
| --------------------- | ------ | ------------------------- | -------------------------------------------------------- |
| `backend.entry`       | string | **yes** (if `backend` present) | Bundle-relative path to the Deno `.ts` entry (no leading `/`, no `..`). |
| `backend.denoVersion` | string | no                        | Target Deno major, e.g. `"2"`.                           |

### update

| Field        | Type   | Required                  | Notes                                                        |
| ------------ | ------ | ------------------------- | ------------------------------------------------------------ |
| `update.url` | string | yes (if `update` present) | URL of a `manifest.json` to poll for a newer `version`. Applying an update is always deliberate, and new permissions force fresh consent. |

## Validation rules

A manifest must pass all of these at install (each fails with a clear message):

- **Strict parse** — unknown fields are rejected.
- **`manifest`** must equal `1`.
- **`id`** must be a reverse-DNS identifier.
- **`version`** must be semver.
- **`minApiVersion`**, if set, must be `"v1"` (not newer than the server).
- At least one of **`ui.panels`** or **`backend.entry`** must be present.
- Each **`panel.slot`** must be a known slot; each **`entry`** must be a relative
  path inside the bundle (no leading `/`, no `..`).
- Each **`permissions.data`** domain and level must be grantable (see
  [permissions.md](permissions.md)); un-grantable or misspelled permissions are
  rejected.
- **`permissions.network`** must be absolute `http(s)` URLs.
- **`permissions.cameras`** must be `"all"` or a list of ids; a camera-touching `data`
  scope (`cameras`/`events`/`coverage`/`segments`/`media`) without a `cameras` request is
  rejected — camera access is opt-in and must be explicit.
- **`settings`** keys must be unique `[a-z0-9._-]`; a `select` field must carry `options`.
- **Backend source** must not contain remote imports (`http(s)://`, `file:`, or an
  absolute path outside the bundle) — rejected at install, not at runtime. Import
  only the bare `@aperture/sdk` specifier and relative files inside your own bundle
  (see [sdk.md](sdk.md)).
