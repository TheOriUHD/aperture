# Aperture extensions

Aperture extensions add functionality to your NVR without a fork or a rebuild. An
extension is a small bundle that can contribute either or both of:

- **UI panels** — HTML/CSS/JS that renders inside the Aperture web app at a fixed
  set of slots (a card on the dashboard, a section in Settings, a panel on a
  camera's detail view, or a full-page view in the nav rail). Each panel runs in a
  **sandboxed iframe** and reaches Aperture only through a typed SDK.
- **A backend** — a headless **Deno** (TypeScript) process that Aperture
  supervises. It reacts to the live change feed and calls the Aperture API, and —
  if you grant it — reaches declared hosts on the network.

One extension can be both. Two worked examples ship in `examples/`:
[`ha-room-lights`](../../examples/ha-room-lights/) — a **UI panel** that toggles the
current camera's Home Assistant light while you watch; and
[`motion-webhook`](../../examples/motion-webhook/) — a **headless backend** that posts
to a webhook when motion is detected on a granted camera, with nobody watching.

## What an extension can and cannot do

Everything an extension may do is declared up front in its `manifest.json` and
shown to you on a consent screen before install. An extension:

- reaches camera data, events, and so on **only** through the Aperture v1 API,
  **only** within the scopes you approve, and **only** for the cameras you approve;
- **never** touches the database, recordings, `secrets.json`, or any other
  extension's files;
- **cannot** run other programs, load native code, or read your server's
  environment.

A backend you grant `network` access to **can send data to the hosts it is allowed
to reach** — that is what backends are for. Grant it only to extensions and hosts
you trust. See [security.md](security.md).

## Bundle format

A bundle is a **zip** with `manifest.json` at the root and two optional folders:

```
my-extension/
  manifest.json      # required — identity, permissions, entry points
  ui/                # optional — panel HTML and assets (web-served)
  backend/           # optional — the Deno entry (never web-served)
```

Full field reference: [manifest.md](manifest.md).

## The private development store

While Aperture is private, the "official store" is a static index served on this host
and read only by `nvrd`. It is real curation, not a stub: an extension listed here
installs through the normal store path and skips the sideload warning.

| | |
|---|---|
| Doc root | `/var/lib/aperture/store/` |
| Server | `aperture-store.service` — `busybox httpd` on **127.0.0.1:8975** |
| Index | `/var/lib/aperture/store/index.json` |
| Pointed at by | the `ext.store_url` setting |

Loopback is deliberate: the browser never fetches the index or a bundle. `GET
/ext/store` does not echo `bundleUrl`, and the install handler resolves and downloads
it server-side, so `nvrd` is the only client these URLs need to reach.

**Layout matters.** An update's bundle is never named anywhere — it is derived as
`bundle.zip` *beside* the manifest that `update.url` points at. So each extension
needs its own directory, and the flat `examples/dist/*.zip` layout cannot be served
as-is:

```
/var/lib/aperture/store/
├── index.json
└── <name>/
    ├── bundle.zip      # the built bundle
    └── manifest.json   # same manifest, for update checks
```

**The index** is decoded into `storeItem` (`internal/api/ext_manager.go`), so it is
exactly these fields — unknown keys are ignored, and `bundleUrl` spelled any other
way parses to empty and installs 404:

```json
{ "extensions": [ { "id": "...", "name": "...", "version": "...",
                    "author": "...", "description": "...",
                    "bundleUrl": "http://127.0.0.1:8975/<name>/bundle.zip" } ] }
```

`version` here is **display-only** — nothing compares or re-installs on it. What
actually drives an update is the version in the manifest at `update.url`.

**Publishing a change** during development:

```bash
make examples                                   # rebuild the zips
install -m 0644 examples/dist/<name>.zip        /var/lib/aperture/store/<name>/bundle.zip
install -m 0644 examples/<name>/manifest.json   /var/lib/aperture/store/<name>/manifest.json
```

No restart of anything: the index is re-read on every request and `busybox httpd`
serves from disk. Bump the manifest `version` for the manager to offer an update.

> **`ext.store_url` has no API, no CLI and no env var** — nothing in the codebase
> writes it, though the Marketplace tells you to set it. Today it can only be set by
> writing the settings row directly:
>
> ```bash
> sqlite3 /var/lib/aperture/data/aperture.db "PRAGMA busy_timeout=5000;
>   INSERT INTO settings(key,value) VALUES('ext.store_url','http://127.0.0.1:8975/index.json')
>   ON CONFLICT(key) DO UPDATE SET value=excluded.value;"
> ```
>
> Use no trailing newline: the "is it configured" check trims the value, but the
> *raw* string is what gets fetched, so stray whitespace reports configured and then
> fails the fetch with a 502.

## Distribution: store vs sideload

|              | Official store                                   | Sideload                                |
| ------------ | ------------------------------------------------ | --------------------------------------- |
| Source       | A curated index the owner reviews and republishes | A file or URL you provide               |
| Review       | Reviewed by the owner before listing             | **Not reviewed by anyone**              |
| Install path | Identical                                        | Identical                               |
| Warning      | Normal consent                                   | Consent **plus an explicit sideload warning** |

The install and permission path is the same either way — curation is what makes
the store safer, not a different mechanism. See
[security.md](security.md#store-vs-sideload).

## Installing

Extensions are an **admin** feature and are **off until you turn them on**:

1. An admin accepts the **global extensions gate** once (a typed confirmation).
   This enables the subsystem.
2. Upload a bundle (sideload) or pick one from the store.
3. Aperture validates the manifest and shows the **consent screen** — the exact
   permissions, the named cameras, and any network hosts.
4. Confirm. Aperture creates the extension, mints its scoped token, extracts its
   files, and (if it has a backend and is enabled) starts it.

Enable/disable, update, edit, and uninstall are all available afterward in the
manager. Disabling stops the backend and revokes its token but keeps its data;
uninstalling deletes its files and its stored data.

## Documentation

- **[manifest.md](manifest.md)** — every manifest field, with the worked Home
  Assistant example.
- **[permissions.md](permissions.md)** — the permission catalogue: what each grants
  and how it reads on the consent screen.
- **[sdk.md](sdk.md)** — the `@aperture/sdk` surface for panels and backends.
- **[settings.md](settings.md)** — declaring an extension's own settings form, with
  write-only secrets.
- **[panels.md](panels.md)** — designing UI panels so they read as part of the app
  (a panel can't inherit the host stylesheet), react to the current camera, and offer
  real controls.
- **[security.md](security.md)** — the sandbox model in plain language: what it
  stops, what it does not.
- **[examples/motion-webhook](../../examples/motion-webhook/)** — the canonical
  **backend** example: react to the change feed, read settings, make one outbound call.
- **[examples/ha-room-lights](../../examples/ha-room-lights/)** — the canonical panel
  bundle you can copy.
