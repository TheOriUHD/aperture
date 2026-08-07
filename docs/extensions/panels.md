# Designing UI panels

A UI panel is a **sandboxed, opaque-origin iframe**. That is what makes it safe (no
cookies, no parent-DOM access, no direct network — see [security.md](security.md)),
but it has a consequence for design: **a panel cannot inherit the host app's
stylesheet**, and its Content-Security-Policy allows only **inline `<style>` and
`<script>`** — no external CSS, fonts, images, or scripts. So a panel must bring its
own styling, and if you want it to look like part of Aperture, mirror Aperture's
tokens yourself.

## Make it read as part of the app

- **Use a transparent body background.** The host renders your panel inside a card on
  a dark surface; a transparent `body` blends into it instead of drawing a bright
  rectangle. Style your own inset elements (rows, sections) on top.
- **Mirror the theme tokens.** Aperture is a dark, restrained UI. Copy these into
  `:root` custom properties and use them rather than ad-hoc colours:

  | Token        | Value     | Use                    |
  | ------------ | --------- | ---------------------- |
  | `--fg`       | `#eef0f2` | primary text           |
  | `--fg-muted` | `#8b9199` | secondary text         |
  | `--fg-faint` | `#6c727a` | captions / hints       |
  | `--surface`  | `#101319` | inset row background    |
  | `--line`     | `#20242b` | borders / dividers      |
  | `--brand`    | `#1b9df0` | accent                  |
  | `--online`   | `#3fce7c` | healthy / on            |
  | `--danger`   | `#f0494e` | errors                  |

  Radii: 6–8px. Base type: `13px/1.5 system-ui`. Spacing rhythm: ~14px padding, ~6–8px
  gaps. Ration colour — let one accent mean something rather than colouring everything.
- **Set `color-scheme: dark`** so form controls and scrollbars render dark.

## Starter stylesheet

Copy this `<style>` block (it is exactly what the `ha-room-lights` example ships):

```html
<style>
  :root {
    color-scheme: dark;
    --fg:#eef0f2; --fg-muted:#8b9199; --fg-faint:#6c727a;
    --surface:#101319; --line:#20242b; --brand:#1b9df0; --online:#3fce7c; --danger:#f0494e;
    --radius:8px;
  }
  * { box-sizing:border-box; }
  body { margin:0; padding:14px; background:transparent; color:var(--fg);
         font:13px/1.5 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif; }
  .row { display:flex; align-items:center; gap:8px; padding:8px 10px;
         border-radius:var(--radius); background:var(--surface); border:1px solid var(--line); }
</style>
```

## Sizing

The host measures your document and sizes the iframe to fit (a `ResizeObserver` calls
back automatically). After first paint — or whenever your content changes height — you
may also nudge it explicitly:

```js
window.aperture.resize(document.documentElement.scrollHeight);
```

Design for a **narrow column** (panels are often docked in a sidebar or under a video),
and let content wrap rather than assuming a wide viewport.

## React to the camera on screen

A `camera-detail` panel is mounted in the context of one camera. The host tells the
panel which one through `window.aperture.cameraId` (null on a non-camera slot) — use it
to make the panel about *what the viewer is looking at* rather than a static list:

```js
const id = window.aperture.cameraId;
const cam = id ? await window.aperture.cameras.get(id) : null; // needs data.cameras: read
```

Where the panel appears is governed by the camera grant, not the panel: an extension
granted only some cameras shows this panel on exactly those (see
[permissions.md](permissions.md#camera-access-is-opt-in)). You do not filter by camera
yourself — you just react to the id you are given.

## Offer a real control (settings + host-mediated fetch)

A panel is for *doing something*, not just displaying. Read the extension's own
configuration from settings, and act on a declared host through `net.fetch` — the host
performs the request server-side, so a **secret** setting can authenticate it without the
panel ever seeing the value:

```js
const base = await window.aperture.settings.get("ha_url");      // non-secret config
const r = await window.aperture.net.fetch(base + "/api/services/light/turn_on", {
  method: "POST",
  headers: { Authorization: "Bearer {{secret:ha_token}}" },      // expanded server-side
  body: JSON.stringify({ entity_id: "light." + window.aperture.cameraId }),
});
// r = { status, body } from the upstream host
```

`{{secret:key}}` may appear in any header value or the body; it is replaced from the
extension's own secret settings by the host, bounded to the declared network host. See
[settings.md](settings.md) and [permissions.md](permissions.md#network). The bundled
`ha-room-lights` example is exactly this: it reads the current camera, its name, and its
configured light, and toggles that light in Home Assistant.

## Keep it self-contained

Everything the panel needs — markup, CSS, JS — lives in the one HTML file (or files you
ship inside `ui/`, referenced relatively). No CDN fonts, no remote images, no analytics.
Encode small images as `data:` URIs if you truly need them (the CSP permits `img-src
data: blob:`). Reach Aperture only through `window.aperture` (see [sdk.md](sdk.md)); the
panel can never call the API directly.
