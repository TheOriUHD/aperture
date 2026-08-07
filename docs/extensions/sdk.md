# The SDK (`@aperture/sdk`)

Panels and backends talk to Aperture through the **same** `aperture` object. What
differs is the transport:

- In a **UI panel**, the host injects `window.aperture` into your served HTML. Each
  call `postMessage`s to the host, which relays it to the server-side broker under
  **your identity ∩ the extension's authority** and posts the reply back. The panel
  never touches cookies or the network directly — the sandbox and CSP forbid it.
- In a **backend**, you `import { aperture } from "@aperture/sdk"`. Each call is an
  HTTP request to the Aperture API carrying the extension's own scoped token.

Same call sites, two transports:

```ts
// Identical in a panel and in a backend:
const cams = await aperture.cameras.list();
const cam  = await aperture.request("GET", "/cameras/hallway");
```

Either way, a call outside the extension's granted scopes or cameras comes back as
an error carrying the broker/API message (HTTP `403`).

## How the backend import resolves (no remote code)

You import the **bare specifier** `@aperture/sdk`. It is **not** a network fetch.
Aperture ships the SDK as a local, read-only module and spawns your backend with a
generated import map (`--import-map`, plus `--allow-read` on the module) that
resolves `@aperture/sdk` to that file. So the import works **verbatim** even though
the backend runs under `--deny-import` (no remote modules).

A backend that contains a **remote** import — `https://…`, `http://…`, `file:`, or
an absolute path outside its bundle — is **rejected at install** with a clear
message, not at runtime with an opaque permission error. Import only
`@aperture/sdk` and relative files inside your own bundle.

## Surface

Not every method exists on both transports. This table is the whole surface:

| Method                          | Panel (`window.aperture`)   | Backend (`@aperture/sdk`) |
| ------------------------------- | --------------------------- | ------------------------- |
| `cameraId` (property)           | ✓ (current camera, or null) | —                         |
| `request(method, path, body?)`  | ✓                           | ✓                         |
| `cameras.list()`                | ✓                           | ✓                         |
| `cameras.get(id)`               | ✓                           | ✓                         |
| `events.list(query)`            | ✓ (query: raw string)       | ✓ (query: object)         |
| `events.get(id)`                | —                           | ✓                         |
| `coverage(id, range?)`          | —                           | ✓                         |
| `automations.list()`            | —                           | ✓                         |
| `alarm.arm(armed)`              | —                           | ✓                         |
| `on(type, handler, opts?)`      | —                           | ✓                         |
| `http.fetch(input, init?)`      | —                           | ✓ (Deno --allow-net)      |
| `net.fetch(url, init?)`         | ✓ (host-mediated)           | —                         |
| `settings.get(key)` / `.all()`  | ✓ (non-secret values)       | via `storage.get`         |
| `storage.get(key)`              | ✓                           | ✓                         |
| `storage.set(key, value)`       | ✓                           | ✓                         |
| `storage.delete(key)`           | ✓                           | ✓                         |
| `storage.keys()`                | ✓                           | ✓                         |
| `resize(height)`                | ✓                           | —                         |

### Common methods (both transports)

**`request<T>(method, path, body?)`** — raw typed call to any v1 endpoint the
extension is scoped for. `path` is relative to `/api/v1`.

```ts
const cams = await aperture.request("GET", "/cameras");
```

**`cameras.list()`** — the cameras the caller may see (viewer ∩ extension in a
panel; extension ∩ owner in a backend).

```ts
const cams = await aperture.cameras.list();
```

**`cameras.get(id)`**

```ts
const cam = await aperture.cameras.get("hallway");
```

**`events.list(query)`** — the one method whose argument differs by transport:

```ts
// Panel: a raw query string with no leading "?"
const evs = await window.aperture.events.list("limit=20");

// Backend: an object of string/number params
const evs = await aperture.events.list({ limit: 20 });
```

### Storage (both transports)

**`storage.get(key)` / `storage.set(key, value)` / `storage.delete(key)` / `storage.keys()`**
— private per-extension key/value storage, enabled by the `storage` permission.
Values are **strings** (JSON-encode structured data yourself). The namespace is
**this extension's alone** — no other extension can read it — and it is **destroyed
when the extension is uninstalled**. It is shared across the app (not per-user) and
bounded by a per-extension quota. `get` resolves to `null` for a missing key.

```ts
// Store a secret once (e.g. from the panel's settings form), read it in the backend.
await aperture.storage.set("ha_token", "llt_xxx");
const token = await aperture.storage.get("ha_token"); // string | null
```

> A key declared as a **`secret`** setting reads back as `null` from a **panel** (secrets
> are write-only to clients) but returns its value to the **backend** (server-side). See
> [settings.md](settings.md).

### Panel-only: current camera, settings, host-mediated fetch

**`aperture.cameraId`** — the camera the panel is mounted for (`camera-detail` slot), or
`null` elsewhere. React to it rather than listing everything.

**`aperture.settings.get(key)` / `aperture.settings.all()`** — the extension's own
settings (the manifest `settings` schema), **non-secret** values only. A scalar returns
a string; a `group` returns an **array of row objects** (e.g. a camera→entity mapping).

**`aperture.net.fetch(url, init?)`** — a request the host performs server-side to a host
the extension may reach (a static `permissions.network` host, or the current value of a
`url` setting via `{{setting:key}}`). A header value or the body may contain a
`{{secret:key}}` placeholder, expanded from the extension's secret settings server-side —
so a secret authenticates the call without ever entering the panel. It resolves to
`{ status, ok, body }` on an upstream reply, or `{ status: 0, ok: false, error, detail }`
on a connection failure (`error` is a plain sentence, `detail` the raw error); it rejects
only on a client-side problem (host not allowed, url setting unconfigured, undeclared
secret).

```ts
const base = await window.aperture.settings.get("ha_url");
const lights = await window.aperture.settings.get("lights"); // [{camera, entity}, …]
const entity = lights.find((l) => l.camera === window.aperture.cameraId)?.entity;
const r = await window.aperture.net.fetch(base + "/api/services/light/turn_on", {
  method: "POST",
  headers: { Authorization: "Bearer {{secret:ha_token}}" },
  body: JSON.stringify({ entity_id: entity }),
});
if (r.status === 0) show(r.error); // legible; r.detail has the raw error
```

### Backend-only methods

**`events.get(id)`**

```ts
const ev = await aperture.events.get("evt_123");
```

**`coverage(cameraId, range?)`** — recording coverage for a camera; `range` is
`{ from?, to? }` (ISO timestamps).

```ts
const cov = await aperture.coverage("hallway", { from: "2026-08-01T00:00:00Z" });
```

**`automations.list()`**

```ts
const autos = await aperture.automations.list();
```

**`alarm.arm(armed)`** — arm (`true`) or disarm (`false`); needs `alarm: "arm"`.

```ts
await aperture.alarm.arm(true);
```

**`on(type, handler, opts?)`** — subscribe to the change feed by envelope `type`.
Returns an **unsubscribe** function and reconnects on drop with 1s→30s backoff.
`opts.cameras` narrows to specific cameras. The server only delivers events the
extension's token may see. Each message is a `FeedMessage`
`{ id, type, ts, camera?, data? }`.

```ts
const off = aperture.on("event.created", (ev) => {
  console.log(ev.type, ev.camera);
}, { cameras: ["hallway"] });
// later: off();
```

**`http.fetch(input, init?)`** — outbound HTTP to a host your `network` permission
allows. A disallowed host throws `NotCapable`. This is the **only** way a backend
reaches the outside world, and it can send your data there.

```ts
await aperture.http.fetch("https://homeassistant.local:8123/api/services/light/turn_on", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ entity_id: "light.hallway" }),
});
```

### Panel-only methods

**`resize(height)`** — ask the host to resize the panel iframe to `height` px. The
injected runtime already calls this on content resize via a `ResizeObserver`; call
it explicitly only to override.

```ts
window.aperture.resize(320);
```

## Errors

**Backend** — a non-2xx response throws `ApertureError` with `.status` (the HTTP
code) and `.body` (the parsed error JSON):

```ts
import { aperture, ApertureError } from "@aperture/sdk";

try {
  await aperture.cameras.get("not-mine");
} catch (e) {
  if (e instanceof ApertureError && e.status === 403) {
    // out of scope or out of camera grant
  }
}
```

**Panel** — `request` / `cameras` / `events` reject with an `Error` carrying the
broker's message (e.g. `403` when out of scope or camera).

## Authoring types

Both transports are typed. For a **backend**, `@aperture/sdk` is fully typed and
passes `deno check`. For a **panel**, import the UI types from `web/src/ext-sdk`
to type `window.aperture` and get autocomplete — panels are served verbatim with no
build step, so the types are for authoring only, kept 1:1 with the injected
runtime.
