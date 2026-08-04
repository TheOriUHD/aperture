import { LiveStream } from "./livestream";
import { cacheStream } from "./streamCache";
import { api } from "./api";
import type { StreamToken } from "./types";

// Keeps ONE WebRTC connection warm for a single camera so that returning from
// recorded playback to the live edge is instant instead of waiting a few seconds
// for a fresh negotiation ("stuck just before live, then slips in"). A detached,
// off-screen <video> owns decoding — so the browser keeps frames flowing — and
// the resulting MediaStream is registered in streamCache, the same registry the
// visible player borrows from for an instant first frame.
//
// WebRTC only: MSE cameras have no shareable MediaStream, so they simply won't
// warm and the visible player connects on its own as before. Best-effort — any
// failure just falls back to today's behavior, never worse.

let curId: string | null = null;
let stream: LiveStream | null = null;
let video: HTMLVideoElement | null = null;
let epoch = 0;

function hiddenVideo(): HTMLVideoElement {
  if (video) return video;
  const v = document.createElement("video");
  v.muted = true;
  v.autoplay = true;
  v.playsInline = true;
  v.setAttribute("playsinline", "");
  // Off-screen but still laid out: display:none suspends the decoder, which
  // would let the cached stream go stale. A 2px transparent, non-interactive box
  // in the corner keeps decoding alive without ever being visible.
  v.style.cssText =
    "position:fixed;left:0;bottom:0;width:2px;height:2px;opacity:0;pointer-events:none;z-index:-1;";
  document.body.appendChild(v);
  video = v;
  return v;
}

// Ensure a warm live connection for `cameraId` (or tear the keeper down when
// null/disabled). Idempotent: calling repeatedly with the same id is a no-op, so
// the connection persists across live/recorded toggles on the same camera.
export function warmLive(cameraId: string | null | undefined, enabled = true) {
  if (!cameraId || !enabled) return stopWarmLive();
  if (cameraId === curId && stream) return;
  stopWarmLive();
  curId = cameraId;
  const mine = ++epoch;
  const v = hiddenVideo();
  void (async () => {
    try {
      const tok = await api.get<StreamToken>(`/api/cameras/${cameraId}/stream-token`);
      if (mine !== epoch) return;
      const proto = location.protocol === "https:" ? "wss" : "ws";
      const url = `${proto}://${location.host}${tok.wsUrl}`;
      const s = new LiveStream(v, url, (st) => {
        if (mine !== epoch) return;
        if (st === "playing" && v.srcObject instanceof MediaStream) {
          cacheStream(cameraId, v.srcObject);
        }
      });
      stream = s;
      s.start();
    } catch {
      /* best-effort: the visible player still connects on its own */
    }
  })();
}

export function stopWarmLive() {
  epoch++;
  curId = null;
  if (stream) {
    stream.destroy();
    stream = null;
  }
  if (video) video.srcObject = null;
}
