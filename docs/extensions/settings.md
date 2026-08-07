# Settings

An extension can declare its own **settings** — the configuration it needs to work
(a host, an API token, a mode). The manager renders the schema as an app-native form
on the extension's row; the values persist in the extension's private storage and are
read back by both SDK transports exactly like storage.

Declaring `settings` **implies** the `storage` permission — you do not write both.

## Schema

`settings` is a top-level array in `manifest.json`. Each field:

```jsonc
"settings": [
  {
    "key": "ha_token",        // [a-z0-9._-]; the key the extension reads back
    "label": "Access token",  // shown on the form
    "type": "secret",         // text | secret | number | boolean | select
    "help": "Create one in …",// optional helper text under the field
    "placeholder": "…",       // optional (text/secret/number)
    "required": true,         // optional; blocks save when empty
    "default": "light.{x}",   // optional; seeded at install (non-secret only)
    "options": [              // REQUIRED for select
      { "value": "a", "label": "Option A" }
    ]
  }
]
```

| Type      | Renders as                       | Stored / read back as                    |
| --------- | -------------------------------- | ---------------------------------------- |
| `text`    | single-line input                | the string                               |
| `secret`  | password input                   | write-only — see below                   |
| `number`  | number input                     | the number, as a string                  |
| `boolean` | a switch                         | `"true"` / `"false"`                     |
| `select`  | a dropdown                       | the chosen option's `value`              |
| `url`     | single-line input, URL-validated | the URL string (can back a network host) |
| `camera`  | a picker of granted cameras      | a camera id                              |
| `group`   | a repeatable list of rows        | a JSON array of row objects — see below  |

Validation runs on save: a bad type, a malformed `url`, a `camera` outside the
extension's grant, or a missing required field is refused with an inline message; a
`select` value must be one of its options. A non-secret scalar `default` is written
into storage at install, so the extension sees it before anyone opens the form.

## Repeatable groups (real configuration)

Flat scalars can't express "map each camera to a light entity". A `group` field is a
named, repeatable list whose rows are themselves a small set of fields (scalars +
`camera`, one level deep — no nested groups or secrets). The form adds/removes rows:

```jsonc
{
  "key": "lights",
  "label": "Camera → light",
  "type": "group",
  "help": "Map each camera to the light to control while viewing it.",
  "fields": [
    { "key": "camera", "label": "Camera", "type": "camera", "required": true },
    { "key": "entity", "label": "Light entity", "type": "text", "required": true }
  ]
}
```

It persists in ext_kv as a JSON array and reads back through the SDK as one — the same
call as a scalar, the value is just structured:

```ts
// panel: an array of row objects
const rows = await aperture.settings.get("lights"); // [{camera, entity}, …]
const mine = rows.find((r) => r.camera === aperture.cameraId);

// backend: the stored JSON string (parse it)
const rows = JSON.parse((await aperture.storage.get("lights")) ?? "[]");
```

## Secrets are write-only

A `secret` field's value is **never returned to a client** — not to a panel, not even
to an admin. The form shows a masked "set" placeholder when one exists; typing a new
value replaces it, and leaving it blank keeps the stored one. The API only ever
reports *whether* a secret is set.

The secret is readable in plaintext by exactly one thing: the extension's **own
backend**, server-side, through its token. A **panel** authenticates to a declared
host without seeing the secret by putting a `{{secret:key}}` placeholder in a
`net.fetch` header or body — the host expands it server-side (see
[panels.md](panels.md)).

## Reading settings

Both transports read a setting like storage:

```ts
// panel (UI) — non-secret values only; a secret reads back as null here
const url = await aperture.settings.get("ha_url");
const all = await aperture.settings.all();

// backend — reads any of its own values, secrets included (server-side)
const token = await aperture.storage.get("ha_token");
```

## Changing settings restarts the backend

Saving settings **restarts** a backend extension so it re-reads them; the form says
so. A UI-only extension has nothing to restart. Changing settings is not a permission
change and never re-prompts consent.

## Network from a setting

A `url` setting can also be the extension's network host: declare
`"network": ["{{setting:ha_url}}"]` and the egress allow-list follows whatever URL the
user saves — the author never hard-codes a host the user didn't choose. See
[permissions.md](permissions.md#network). Changing the URL changes the allow-list
immediately (no re-consent); an unset or invalid URL refuses egress with a clear
message.

## Example

The bundled [ha-room-lights](../../examples/ha-room-lights/) example uses a `url`
(the Home Assistant address, and its only network host), a `secret` token, and a
`group` mapping each camera to a light entity. Its panel looks up the camera on screen
in that mapping and toggles its light, authenticating with the secret via a
`{{secret:ha_token}}` placeholder.
