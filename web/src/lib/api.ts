// Thin fetch wrapper for the Aperture JSON API. Sessions ride on an httpOnly
// cookie, so we just send credentials and surface errors uniformly.

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

// v1Path upgrades a legacy "/api/…" path to "/api/v1/…" (Phase 5.2). The two are
// the same handlers today; funnelling every call through here migrates them all
// at once and future-proofs for a diverging v2. Idempotent; non-/api untouched.
export function v1Path(path: string): string {
  return path.startsWith("/api/") && !path.startsWith("/api/v1/")
    ? "/api/v1/" + path.slice("/api/".length)
    : path;
}

// ETag cache for optimistic concurrency (1.9 / 5.2). The server stamps a strong
// ETag on config-resource GETs (site, settings, detection, per-camera zones and
// schedule, a single camera). We remember it per path and replay it as If-Match
// on the next write to that SAME path, so a config save that raced another
// editor's change is rejected with 412 ("it changed since you read it") instead
// of silently clobbering it. The server treats If-Match as optional for the web's
// cookie sessions, so this is purely additive: a path we hold no ETag for sends
// none and keeps last-writer-wins. A write's reply refreshes the tag (the camera
// PUT echoes a fresh ETag); a write with no ETag in the reply drops the cached one
// so we never replay a validator we know is now stale — the next GET repopulates.
const etagCache = new Map<string, string>();
const etagKey = (path: string) => v1Path(path).split("?")[0];
const isWrite = (m: string) => m === "PUT" || m === "POST" || m === "PATCH" || m === "DELETE";

// The server stamps a per-item `etag` onto objects it returns in list responses
// (Item 2) for If-Match round-trips. That tag is metadata, never a request input:
// writes are decoded with DisallowUnknownFields, so echoing it back in a body is a
// hard 400 ("invalid request body"). The web frequently edits by spreading a
// fetched object ({ ...item, changed }), so strip the envelope tag here — at the
// one boundary every write passes through — instead of trusting each call site to
// remember. It is deliberately NOT lifted into If-Match: list items carry
// server-mutated fields (e.g. an automation's lastFired), so the tag can be stale
// for benign reasons and would cause spurious 412s; last-writer-wins stays the
// right default for these until a body-etag concurrency story lands. (The
// header-based If-Match above, keyed off config GETs, is unaffected.)
function stripServerEnvelope(body: unknown): unknown {
  if (body && typeof body === "object" && !Array.isArray(body) && "etag" in body) {
    const clone = { ...(body as Record<string, unknown>) };
    delete clone.etag;
    return clone;
  }
  return body;
}

async function request<T>(method: string, path: string, body?: unknown, opts?: { ifMatch?: string }): Promise<T> {
  const key = etagKey(path);
  const headers: Record<string, string> = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (isWrite(method)) {
    // An explicit If-Match — a collection item's per-item etag read from the LIST
    // body — wins; else fall back to the path-keyed etag captured from a singleton
    // GET's response header. Sent as a header, never in the body (the server's
    // DisallowUnknownFields would 400 an etag field; stripServerEnvelope guards it).
    const tag = opts?.ifMatch ?? etagCache.get(key);
    if (tag) headers["If-Match"] = tag;
  }
  const res = await fetch(v1Path(path), {
    method,
    credentials: "same-origin",
    headers: Object.keys(headers).length ? headers : undefined,
    body: body !== undefined ? JSON.stringify(stripServerEnvelope(body)) : undefined,
  });
  const et = res.headers.get("ETag");
  if (et) {
    etagCache.set(key, et);
  } else if (isWrite(method) && res.ok) {
    etagCache.delete(key); // resource changed but reply didn't echo a tag → refetch will repopulate
  }
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) {
    throw new ApiError(res.status, (data && data.error) || res.statusText);
  }
  return data as T;
}

export const api = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T>(path: string, body?: unknown) => request<T>("POST", path, body ?? {}),
  put: <T>(path: string, body?: unknown, opts?: { ifMatch?: string }) => request<T>("PUT", path, body ?? {}, opts),
  del: <T>(path: string, opts?: { ifMatch?: string }) => request<T>("DELETE", path, undefined, opts),
};

// downloadFile streams a binary endpoint to a browser save dialog. The JSON
// `request` above JSON-parses every response, so blobs must bypass it.
export async function downloadFile(path: string, fallbackName: string): Promise<void> {
  const res = await fetch(path, { credentials: "same-origin" });
  if (!res.ok) throw new ApiError(res.status, res.statusText);
  const blob = await res.blob();
  const cd = res.headers.get("Content-Disposition") || "";
  const m = /filename=([^;]+)/.exec(cd);
  const name = m ? m[1].trim().replace(/["']/g, "") : fallbackName;
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  // Revoke after the browser has had a chance to start the download.
  setTimeout(() => {
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }, 1000);
}

// uploadFile posts a single file as multipart/form-data and parses a JSON reply.
export async function uploadFile<T>(path: string, field: string, file: File): Promise<T> {
  const fd = new FormData();
  fd.append(field, file);
  const res = await fetch(path, { method: "POST", credentials: "same-origin", body: fd });
  const text = await res.text();
  let data: unknown = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    if (!res.ok) throw new ApiError(res.status, res.statusText);
    throw new ApiError(res.status, "unexpected response from server");
  }
  if (!res.ok) {
    throw new ApiError(res.status, (data as { error?: string })?.error || res.statusText);
  }
  return data as T;
}
