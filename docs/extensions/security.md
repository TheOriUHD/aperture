# Security model

This is the plain-language version of what the extension sandbox does and does not
protect. Read it before you grant `network` or sideload anything.

## What it stops

An extension **cannot**:

- read your recordings, your database, your passwords/secrets, or the files of
  Aperture or of any other extension;
- run other programs, load native code, or read your server's environment (a
  backend sees only the two values Aperture injects for it — its own API token and
  the API base URL — nothing else from the host);
- reach the Aperture API outside the exact permissions you approved, for any camera
  you did not approve;
- take down recording — a crash or memory leak in an extension cannot stop the
  recorder.

A UI panel additionally cannot touch cookies, the parent page, or the network
directly: it runs in an iframe with `sandbox="allow-scripts"` (deliberately without
same-origin) under a strict Content-Security-Policy, so its only path to Aperture
is the sanctioned, scope-checked broker.

## What it does NOT stop

- A **backend you grant `network` access to can send data to the hosts it is
  allowed to reach.** That is what backend extensions are for; the container cannot
  block it. Grant `network` only to extensions and hosts you trust.
- **Sideloaded extensions are not reviewed by anyone.** When you sideload, you are
  vouching for the author yourself.

Confinement is two layers — Deno's permission model (what the code can reach) and a
non-root, resource-limited per-extension user (what a runtime escape could do) — but
neither the platform nor this document can stop a trusted backend from doing what
you trusted it to do.

## Per-viewer authority

A UI panel is capped by **each viewer's own permissions**, never the installer's.
The broker re-dispatches every panel call under **the calling user's identity
intersected with the extension's declared authority**:

- extension scopes ∩ the viewer's scopes,
- extension cameras ∩ the viewer's cameras,
- and admin-gated routes still require **the viewer** to be an admin.

So the **same panel may show less to a viewer than to an admin** — a one-camera
viewer sees one camera even if the extension was installed with all-camera access.
**That is correct behavior, not a bug.** A UI extension can never do more for a user
than that user could do unaided, and never more than its manifest declared. The
extension's token never enters the browser.

## Store vs sideload

The install and permission path is **identical** for both. The difference is trust:

- **Store** items were **reviewed and republished by the owner**. They install with
  the normal consent screen.
- **Sideloaded** items came from a file or URL you provided and are **not reviewed
  by anyone**. They install with the same consent screen **plus an explicit
  sideload warning**.

Curation is what makes the store safer — not a different sandbox. A sideloaded
extension is confined exactly as a store one is; you are simply the one vouching
for it.

## Updates

Applying an update is always a deliberate action, never silent. An update that asks
for **new** permissions forces **fresh consent**, and the previous version is kept
so you can revert. Because a backend is confined to the permissions you approve,
even a malicious update can only do what its (re-)consented manifest allows.
