// A tiny client-side registry of connected WebRTC MediaStreams, keyed by camera.
// A single MediaStream can feed multiple <video> elements, so when a second view
// of the same camera opens (e.g. the pop-out over a live tile), it can attach the
// already-connected stream for an INSTANT first frame instead of showing the
// connecting-dots while a fresh WebRTC negotiation completes.
//
// Only WebRTC streams are cacheable (MSE has no shareable MediaStream). Stale
// entries (all tracks ended) are dropped on read.

const cache = new Map<string, MediaStream>();

// Keyed by camera AND quality ("main"/"sub"), so switching quality never attaches
// the wrong-resolution stream.
const key = (cameraID: string, quality = "main") => `${cameraID}:${quality}`;

export function cacheStream(cameraID: string, ms: MediaStream | null | undefined, quality = "main") {
  if (ms && ms.getVideoTracks().some((t) => t.readyState === "live")) {
    cache.set(key(cameraID, quality), ms);
  }
}

export function getCachedStream(cameraID: string, quality = "main"): MediaStream | null {
  const k = key(cameraID, quality);
  const ms = cache.get(k);
  if (!ms) return null;
  if (!ms.getVideoTracks().some((t) => t.readyState === "live")) {
    cache.delete(k);
    return null;
  }
  return ms;
}

// Drop a camera's cached stream (all qualities). Used when a resumed connection
// is suspect — a track can read "live" yet deliver no frames after a background
// suspend, so we don't want to hand it back for an "instant" (but black) view.
export function clearCachedStream(cameraID: string) {
  for (const k of cache.keys()) {
    if (k.startsWith(`${cameraID}:`)) cache.delete(k);
  }
}

export function clearAllCachedStreams() {
  cache.clear();
}
