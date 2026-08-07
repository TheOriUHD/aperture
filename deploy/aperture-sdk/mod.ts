// @aperture/sdk — the Aperture extension SDK, backend (Deno) transport.
//
// Authors import the bare specifier:
//
//     import { aperture } from "@aperture/sdk";
//
// which the supervisor's generated import map resolves to this local, read-only
// module (see internal/ext/spec.go: WriteImportMap + --import-map), so it works
// under --deny-import — no remote code is ever fetched. This file is version-matched
// to the server it ships with.
//
// Every method is a typed wrapper over the Aperture v1 HTTP API, called with the
// extension's own scoped token. The server re-checks the extension's manifest scopes
// ∩ owner on each request, so nothing here can exceed the consent granted at install:
// a call outside the granted scopes or cameras comes back as ApertureError(403).

/** Read one of the two vars the supervisor injects. The child's --allow-env is
 * scoped to exactly APERTURE_API + APERTURE_TOKEN, so any other key throws and is
 * swallowed to "". */
function env(k: string): string {
  try {
    return Deno.env.get(k) ?? "";
  } catch {
    return "";
  }
}
const API_BASE = env("APERTURE_API"); // e.g. http://localhost:8080
const TOKEN = env("APERTURE_TOKEN"); // apt_… (this extension's scoped token)
const EXT_ID = env("APERTURE_EXT_ID"); // this extension's id — its private KV namespace

/** A non-2xx API response. `status` is the HTTP code, `body` the parsed error JSON. */
export class ApertureError extends Error {
  readonly status: number;
  readonly body: unknown;
  constructor(status: number, message: string, body?: unknown) {
    super(message);
    this.name = "ApertureError";
    this.status = status;
    this.body = body;
  }
}

// Minimal hand-written shapes — a subset of the fields, with an index signature so
// unknown fields survive. The full types are generated from the OpenAPI in a later
// task; these keep the common calls ergonomic without pretending to be exhaustive.
export interface Camera {
  id: string;
  name: string;
  enabled?: boolean;
  [k: string]: unknown;
}
export interface ApertureEvent {
  id: string;
  type?: string;
  camera?: string;
  startedAt?: string;
  [k: string]: unknown;
}
/** One change-feed envelope: {id, type, ts, camera, data}. */
export interface FeedMessage {
  id: number;
  type: string;
  ts: number;
  camera?: string;
  data?: unknown;
}

/** v1-prefix a path unless it is already absolute under /api. */
function v1(path: string): string {
  if (!path.startsWith("/")) path = "/" + path;
  return path.startsWith("/api/") ? path : "/api/v1" + path;
}

const enc = encodeURIComponent;
function kvPath(key: string): string {
  return `/ext/${enc(EXT_ID)}/kv/${enc(key)}`;
}

function parse(text: string): unknown {
  try {
    return text ? JSON.parse(text) : undefined;
  } catch {
    return text;
  }
}

function toQuery(q?: Record<string, string | number | undefined>): string {
  if (!q) return "";
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(q)) if (v !== undefined) p.set(k, String(v));
  const s = p.toString();
  return s ? "?" + s : "";
}

/** Core request: attaches the bearer token + JSON headers and unwraps API errors. */
async function request<T = unknown>(method: string, path: string, body?: unknown): Promise<T> {
  if (!API_BASE || !TOKEN) {
    throw new ApertureError(0, "SDK not initialised: APERTURE_API/APERTURE_TOKEN missing");
  }
  const headers: Record<string, string> = { Authorization: "Bearer " + TOKEN };
  let payload: string | undefined;
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  const res = await fetch(API_BASE + v1(path), { method, headers, body: payload });
  const data = parse(await res.text());
  if (!res.ok) {
    const msg = data && typeof data === "object" && data !== null && "error" in data
      ? String((data as Record<string, unknown>).error)
      : `HTTP ${res.status}`;
    throw new ApertureError(res.status, msg, data);
  }
  return data as T;
}

/** Subscribe to the change feed (SSE) by envelope type. Returns an unsubscribe
 * function. Reconnects with 1s→30s backoff on drop. The server filters by type and
 * camera; the callback still re-checks `type` client-side. */
function on(
  type: string,
  handler: (msg: FeedMessage) => void,
  opts?: { cameras?: string[] },
): () => void {
  let stopped = false;
  let controller: AbortController | null = null;
  const qs = new URLSearchParams({ types: type });
  if (opts?.cameras?.length) qs.set("cameras", opts.cameras.join(","));

  async function run() {
    let backoff = 1000;
    while (!stopped) {
      controller = new AbortController();
      try {
        const res = await fetch(API_BASE + v1("/feed?" + qs.toString()), {
          headers: { Authorization: "Bearer " + TOKEN, Accept: "text/event-stream" },
          signal: controller.signal,
        });
        if (!res.ok || !res.body) throw new ApertureError(res.status, "feed connect failed");
        backoff = 1000;
        const reader = res.body.getReader();
        const dec = new TextDecoder();
        let buf = "";
        while (!stopped) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += dec.decode(value, { stream: true });
          let nl: number;
          while ((nl = buf.indexOf("\n\n")) >= 0) {
            const frame = buf.slice(0, nl);
            buf = buf.slice(nl + 2);
            for (const line of frame.split("\n")) {
              if (!line.startsWith("data:")) continue;
              const msg = parse(line.slice(5).trim()) as FeedMessage | undefined;
              if (msg && typeof msg === "object" && msg.type === type) {
                try {
                  handler(msg);
                } catch {
                  /* handler faults are the extension's own problem, not ours */
                }
              }
            }
          }
        }
      } catch {
        /* fall through to backoff */
      }
      if (stopped) break;
      await new Promise((r) => setTimeout(r, backoff));
      backoff = Math.min(backoff * 2, 30000);
    }
  }
  void run();
  return () => {
    stopped = true;
    controller?.abort();
  };
}

export const aperture = {
  /** Raw typed call to any v1 endpoint the extension is scoped for. */
  request,

  cameras: {
    list: () => request<Camera[]>("GET", "/cameras"),
    get: (id: string) => request<Camera>("GET", `/cameras/${encodeURIComponent(id)}`),
  },
  events: {
    list: (query?: Record<string, string | number>) =>
      request<ApertureEvent[]>("GET", "/events" + toQuery(query)),
    get: (id: string) => request<ApertureEvent>("GET", `/events/${encodeURIComponent(id)}`),
  },
  coverage: (cameraId: string, range?: { from?: string; to?: string }) =>
    request("GET", `/cameras/${encodeURIComponent(cameraId)}/coverage` + toQuery(range)),
  automations: {
    list: () => request("GET", "/automations"),
  },
  alarm: {
    arm: (armed: boolean) => request<{ armed: boolean }>("PUT", "/alarm", { armed }),
  },

  /** Private per-extension key/value storage (the `storage` permission). Namespaced
   * to THIS extension — no other extension can read it — and destroyed when the
   * extension is uninstalled. Values are strings; JSON-encode structured data
   * yourself. Bounded by a per-extension quota. */
  storage: {
    get: async (key: string): Promise<string | null> => {
      try {
        const r = await request<{ key: string; value: string }>("GET", kvPath(key));
        return r.value;
      } catch (e) {
        if (e instanceof ApertureError && e.status === 404) return null; // absent key
        throw e;
      }
    },
    set: (key: string, value: string) => request<void>("PUT", kvPath(key), { value }),
    delete: (key: string) => request<void>("DELETE", kvPath(key)),
    keys: () => request<{ keys: string[] }>("GET", `/ext/${enc(EXT_ID)}/kv`).then((r) => r.keys),
  },

  /** Subscribe to change-feed events by type. Returns an unsubscribe function. */
  on,

  /** Outbound HTTP to a host the manifest's `network` permission allows. Deno's
   * --allow-net enforces the allowlist — a disallowed host throws NotCapable. This
   * is the ONLY way a backend extension reaches the outside world, and it can send
   * your data there, so grant `network` only to extensions and hosts you trust. */
  http: {
    fetch: (input: string | URL, init?: RequestInit) => fetch(input, init),
  },
};

export default aperture;
