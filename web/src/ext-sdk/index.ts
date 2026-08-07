// @aperture/sdk — typed surface of the UI (iframe) SDK.
//
// A UI panel runs inside a sandboxed iframe; the host injects a runtime that exposes
// `window.aperture` (see internal/api/ext_static.go: extSDKBootstrap). Panels are
// served verbatim with no build step, so this file exists for AUTHORING: import
// these types in your panel's TypeScript to get autocomplete and a compile-time check
// that you call the SDK exactly as the runtime provides it. The shape here is kept
// 1:1 with the injected bootstrap.
//
// Every call postMessages to the host, which relays it to POST /api/v1/ext/{id}/broker
// under the caller∩extension authority and posts the reply back. The panel never
// touches cookies or the network directly — the sandbox (no allow-same-origin) and
// the per-response CSP forbid it — so this is the only path to Aperture data. It
// therefore shows each viewer only what THAT viewer may see: a panel can legitimately
// show less to a one-camera viewer than to an admin, and that is correct, not a bug.

export interface UiAperture {
  /** The camera whose view this panel is mounted in (from the host), or null on a
   * non-camera slot. Use it to make a camera-detail panel react to what's on screen. */
  readonly cameraId: string | null;
  /** Raw call to any v1 endpoint the extension is scoped for. `path` is relative to
   * /api/v1 (e.g. "/cameras", "/events?limit=20"). Rejects with an Error carrying the
   * broker's message on a non-2xx reply (e.g. 403 when out of scope/camera). */
  request<T = unknown>(method: string, path: string, body?: unknown): Promise<T>;
  cameras: {
    /** The cameras this viewer + this extension may both see. */
    list<T = unknown>(): Promise<T>;
    get<T = unknown>(id: string): Promise<T>;
  };
  events: {
    /** `query` is a raw query string without the leading "?" (e.g. "limit=20"). */
    list<T = unknown>(query?: string): Promise<T>;
  };
  /** Private per-extension key/value storage (the `storage` permission). Namespaced
   * to this extension and shared across the app; destroyed on uninstall. Values are
   * strings — JSON-encode structured data yourself. A key declared as a `secret`
   * setting reads back as null here — secrets are write-only to a client. */
  storage: {
    get(key: string): Promise<string | null>;
    set(key: string, value: string): Promise<void>;
    delete(key: string): Promise<void>;
    keys(): Promise<string[]>;
  };
  /** The extension's own settings (the manifest `settings` schema), configured by an
   * admin on the extension's row. `get` returns a NON-secret value — a string for a
   * scalar, or an array of row objects for a `group` (e.g. a camera→entity mapping),
   * or null if unset. Secret values are never exposed to a panel — authenticate to a
   * declared host with a {{secret:key}} placeholder in `net.fetch` instead. */
  settings: {
    get<T = string | Record<string, string>[]>(key: string): Promise<T | null>;
    all(): Promise<Record<string, string | Record<string, string>[]>>;
  };
  /** Host-mediated outbound request (the `network` permission). The host performs it
   * server-side to a host the extension is allowed to reach — a static manifest host,
   * or the current value of a url setting (`{{setting:key}}`). A header value or the
   * body may contain a `{{secret:key}}` placeholder, expanded from the extension's own
   * secret settings server-side, so a token authenticates the call without ever
   * entering the panel.
   *
   * Resolves to `{ status, ok, body }` on an upstream response. On a connection failure
   * it resolves (does not reject) with `{ status: 0, ok: false, error, detail }` — a
   * plain sentence in `error` (show this), the raw error in `detail` (available, not
   * front and centre). It rejects only on a client-side problem (host not allowed, the
   * url setting unconfigured, an undeclared secret). */
  net: {
    fetch(
      url: string,
      init?: { method?: string; headers?: Record<string, string>; body?: string },
    ): Promise<{ status: number; ok: boolean; body?: string; error?: string; detail?: string }>;
  };
  /** Ask the host to resize this panel's iframe to `height` px. The runtime already
   * calls this on content resize via a ResizeObserver; call it explicitly only to
   * override. */
  resize(height: number): void;
}

declare global {
  interface Window {
    /** Injected by the host into every served extension HTML entry. */
    aperture: UiAperture;
  }
}

/** Convenience typed accessor — equivalent to `window.aperture`, available once the
 * injected bootstrap has run (it is injected into <head>, before your panel script). */
export const aperture: UiAperture =
  (globalThis as unknown as { aperture: UiAperture }).aperture;

export default aperture;
