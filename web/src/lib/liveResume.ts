// Fires subscribers when the app returns to the foreground — tab re-focused, PWA
// resumed from background, bfcache restore, or the network coming back. Mobile
// browsers (and PWAs) freeze/kill WebRTC + MSE connections while backgrounded, so
// this is the moment every live player must re-check and reconnect any stalled
// feed. Without it you get the classic "feeds are black until I refresh" on reopen.

type Handler = () => void;
const handlers = new Set<Handler>();
let wired = false;
let pending = 0;

function fire() {
  if (pending) return; // collapse a burst of resume events into one
  pending = window.setTimeout(() => {
    pending = 0;
    handlers.forEach((h) => {
      try {
        h();
      } catch {
        /* a bad handler must not block the others */
      }
    });
  }, 60);
}

function wire() {
  if (wired || typeof document === "undefined") return;
  wired = true;
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") fire();
  });
  // pageshow fires on bfcache restore (back/forward) and iOS PWA resume; focus
  // covers desktop tab switches; online covers a dropped-then-restored network.
  window.addEventListener("pageshow", fire);
  window.addEventListener("focus", fire);
  window.addEventListener("online", fire);
}

// Subscribe to foreground/resume events. Returns an unsubscribe fn.
export function onLiveResume(handler: Handler): () => void {
  wire();
  handlers.add(handler);
  return () => handlers.delete(handler);
}
