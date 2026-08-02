// A tiny client-side registry of connected WebRTC MediaStreams, keyed by camera.
// A single MediaStream can feed multiple <video> elements, so when a second view
// of the same camera opens (e.g. the pop-out over a live tile), it can attach the
// already-connected stream for an INSTANT first frame instead of showing the
// connecting-dots while a fresh WebRTC negotiation completes.
//
// Only WebRTC streams are cacheable (MSE has no shareable MediaStream). Stale
// entries (all tracks ended) are dropped on read.

const cache = new Map<string, MediaStream>();

export function cacheStream(cameraID: string, ms: MediaStream | null | undefined) {
  if (ms && ms.getVideoTracks().some((t) => t.readyState === "live")) {
    cache.set(cameraID, ms);
  }
}

export function getCachedStream(cameraID: string): MediaStream | null {
  const ms = cache.get(cameraID);
  if (!ms) return null;
  if (!ms.getVideoTracks().some((t) => t.readyState === "live")) {
    cache.delete(cameraID);
    return null;
  }
  return ms;
}
