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
   * strings — JSON-encode structured data yourself. */
  storage: {
    get(key: string): Promise<string | null>;
    set(key: string, value: string): Promise<void>;
    delete(key: string): Promise<void>;
    keys(): Promise<string[]>;
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
