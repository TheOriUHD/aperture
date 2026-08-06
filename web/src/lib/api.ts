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

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(v1Path(path), {
    method,
    credentials: "same-origin",
    headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
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
  put: <T>(path: string, body?: unknown) => request<T>("PUT", path, body ?? {}),
  del: <T>(path: string) => request<T>("DELETE", path),
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
