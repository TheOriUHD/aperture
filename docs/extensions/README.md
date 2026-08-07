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

One extension can be both. The canonical example,
[`examples/ha-room-lights`](../../examples/ha-room-lights/), adds a panel to the
camera-detail view **and** turns on that room's Home Assistant lights while you
watch.

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
- **[panels.md](panels.md)** — designing UI panels so they read as part of the app
  (a panel can't inherit the host stylesheet).
- **[security.md](security.md)** — the sandbox model in plain language: what it
  stops, what it does not.
- **[examples/ha-room-lights](../../examples/ha-room-lights/)** — the canonical
  bundle you can copy.
