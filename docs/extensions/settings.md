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

| Type      | Renders as        | Stored / read back as              |
| --------- | ----------------- | ---------------------------------- |
| `text`    | single-line input | the string                         |
| `secret`  | password input    | write-only — see below             |
| `number`  | number input      | the number, as a string            |
| `boolean` | a switch          | `"true"` / `"false"`               |
| `select`  | a dropdown        | the chosen option's `value`        |

Validation runs on save: a bad type or a missing required field is refused with an
inline message; a `select` value must be one of its options. A non-secret `default`
is written into storage at install, so the extension sees it before anyone opens the
form.

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

## Example

The bundled [ha-room-lights](../../examples/ha-room-lights/) example uses three
settings — a `text` host URL, a `secret` token, and a `text` entity template — and its
panel toggles the current camera's light in Home Assistant using the secret via a
`{{secret:ha_token}}` placeholder.
