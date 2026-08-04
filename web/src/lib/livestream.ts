// Self-contained live player engine for go2rtc's WebSocket API, proxied through
// nvrd. Tries WebRTC first (lowest latency), then falls back to MSE/fMP4. No
// third-party code — we speak the documented go2rtc message protocol directly:
//   client -> {type:"webrtc/offer"|"webrtc/candidate"|"mse", value}
//   server -> {type:"webrtc/answer"|"webrtc/candidate"|"mse", value}

import { onLiveResume } from "./liveResume";

export type LiveState = "connecting" | "playing" | "error";

// How long the picture may be frozen (currentTime not advancing) before we treat
// the connection as dead and reconnect. Covers both WebRTC and MSE.
const STALL_MS = 6000;

const MSE_CODECS = [
  "avc1.640029", // H.264 High
  "avc1.64002A",
  "avc1.640033",
  "hvc1.1.6.L153.B0", // H.265
  "mp4a.40.2", // AAC LC
  "mp4a.40.5",
  "flac",
  "opus",
];

type MediaSourceCtor = {
  new (): MediaSource;
  isTypeSupported(type: string): boolean;
};

export class LiveStream {
  private ws?: WebSocket;
  private pc?: RTCPeerConnection;
  private mode: "webrtc" | "mse" = "webrtc";
  private destroyed = false;
  private timer?: number;

  // Self-heal state.
  private playing = false; // has presented at least one frame this session
  private retries = 0;
  private watchdog?: number;
  private reconnectTimer?: number;
  private lastTime = -1;
  private lastAdvanceAt = 0;
  private unresume?: () => void;

  constructor(
    private video: HTMLVideoElement,
    private url: string,
    private onState: (s: LiveState) => void,
  ) {}

  start() {
    // Reconnect on foreground/resume (mobile freezes connections in the
    // background) and watch for a frozen picture the whole time we're alive.
    this.unresume = onLiveResume(() => this.resumeCheck());
    this.startWatchdog();
    this.onState("connecting");
    this.connectWebRTC();
  }

  destroy() {
    this.destroyed = true;
    this.unresume?.();
    if (this.watchdog) window.clearInterval(this.watchdog);
    if (this.reconnectTimer) window.clearTimeout(this.reconnectTimer);
    this.teardown();
  }

  // teardown closes the transport but leaves the watchdog/resume hook in place so
  // a reconnect can reuse them; it also keeps the last srcObject so the frozen
  // frame stays covered by the placeholder rather than flashing black.
  private teardown() {
    this.clearTimer();
    // Detach first so our own close() (which fires "closed"/onclose) is ignored by
    // the stale-guard in the handlers below and can't trigger a spurious reconnect.
    const ws = this.ws;
    const pc = this.pc;
    this.ws = undefined;
    this.pc = undefined;
    try {
      ws?.close();
    } catch {
      /* ignore */
    }
    try {
      pc?.close();
    } catch {
      /* ignore */
    }
  }

  private clearTimer() {
    if (this.timer) window.clearTimeout(this.timer);
    this.timer = undefined;
  }

  private markPlaying() {
    this.playing = true;
    this.retries = 0;
    this.lastAdvanceAt = Date.now();
    this.clearTimer();
    this.onState("playing");
  }

  // Full restart (WebRTC first again) with capped backoff — used when a live
  // connection drops or freezes, distinct from fail() which is the first-connect
  // WebRTC→MSE fallback.
  private reconnect() {
    if (this.destroyed || this.reconnectTimer) return;
    this.teardown();
    this.playing = false;
    this.mode = "webrtc";
    this.lastAdvanceAt = Date.now();
    this.onState("connecting");
    const delay = Math.min(400 * 2 ** this.retries, 4000);
    this.retries = Math.min(this.retries + 1, 6);
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = undefined;
      if (!this.destroyed) this.connectWebRTC();
    }, delay);
  }

  // Polls whether the picture is actually advancing; a frozen feed (dead peer,
  // resumed-from-background socket) reconnects itself.
  private startWatchdog() {
    if (this.watchdog) return;
    this.lastTime = -1;
    this.lastAdvanceAt = Date.now();
    this.watchdog = window.setInterval(() => {
      if (this.destroyed || !this.playing) return;
      const t = this.video.currentTime;
      if (t !== this.lastTime) {
        this.lastTime = t;
        this.lastAdvanceAt = Date.now();
      } else if (!this.video.paused && Date.now() - this.lastAdvanceAt > STALL_MS) {
        // Frozen while trying to play → dead connection. (A deliberately paused
        // video is left alone — reconnecting wouldn't un-pause it.)
        this.reconnect();
      }
    }, 2500);
  }

  // Called the instant the app returns to the foreground: nudge autoplay, and if
  // the picture doesn't advance shortly after, reconnect immediately rather than
  // waiting for the watchdog.
  private resumeCheck() {
    if (this.destroyed) return;
    void this.video.play().catch(() => undefined);
    if (!this.playing) return;
    const t = this.video.currentTime;
    window.setTimeout(() => {
      if (this.destroyed || !this.playing) return;
      if (this.video.currentTime === t && !this.video.paused) this.reconnect();
    }, 700);
  }

  private fail() {
    if (this.destroyed) return;
    if (this.mode === "webrtc") {
      this.teardown();
      this.mode = "mse";
      this.connectMSE();
    } else {
      this.onState("error");
    }
  }

  // --- WebRTC (primary) -----------------------------------------------------
  private connectWebRTC() {
    const pc = new RTCPeerConnection({ iceServers: [], bundlePolicy: "max-bundle" });
    this.pc = pc;
    const stream = new MediaStream();
    pc.addTransceiver("video", { direction: "recvonly" });
    pc.addTransceiver("audio", { direction: "recvonly" });

    pc.ontrack = (ev) => {
      if (pc !== this.pc) return;
      stream.addTrack(ev.track);
      this.video.srcObject = stream;
      void this.video.play().catch(() => undefined);
    };
    pc.onconnectionstatechange = () => {
      if (pc !== this.pc) return; // event from a superseded attempt
      const st = pc.connectionState;
      if (st === "connected") {
        this.markPlaying();
      } else if (st === "failed") {
        // Before first frame: fall back to MSE. After: the connection died — full
        // reconnect. ("disconnected" is left to the watchdog, as ICE often recovers.)
        if (this.playing) this.reconnect();
        else this.fail();
      } else if (st === "closed" && this.playing) {
        this.reconnect();
      }
    };

    const ws = new WebSocket(this.url);
    this.ws = ws;
    ws.binaryType = "arraybuffer";

    pc.onicecandidate = (ev) => {
      if (ev.candidate && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "webrtc/candidate", value: ev.candidate.candidate }));
      }
    };
    ws.onopen = async () => {
      if (ws !== this.ws) return;
      try {
        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
        ws.send(JSON.stringify({ type: "webrtc/offer", value: offer.sdp }));
      } catch {
        this.fail();
      }
    };
    ws.onmessage = (ev) => {
      if (ws !== this.ws) return;
      if (typeof ev.data !== "string") return;
      let msg: { type: string; value: string };
      try {
        msg = JSON.parse(ev.data);
      } catch {
        return;
      }
      if (msg.type === "webrtc/answer") {
        pc.setRemoteDescription({ type: "answer", sdp: msg.value }).catch(() => this.fail());
      } else if (msg.type === "webrtc/candidate" && msg.value) {
        pc.addIceCandidate({ candidate: msg.value, sdpMid: "0" }).catch(() => undefined);
      } else if (msg.type === "error") {
        this.fail();
      }
    };
    ws.onerror = () => {
      if (ws !== this.ws) return;
      this.fail();
    };

    this.timer = window.setTimeout(() => {
      if (pc === this.pc && pc.connectionState !== "connected") this.fail();
    }, 5000);
  }

  // --- MSE / fMP4 (fallback) ------------------------------------------------
  private connectMSE() {
    this.onState("connecting");
    this.video.srcObject = null;

    const MS: MediaSourceCtor =
      (window as unknown as { ManagedMediaSource?: MediaSourceCtor }).ManagedMediaSource ||
      (window.MediaSource as unknown as MediaSourceCtor);

    const ws = new WebSocket(this.url);
    this.ws = ws;
    ws.binaryType = "arraybuffer";

    let sb: SourceBuffer | undefined;
    const queue: ArrayBuffer[] = [];
    let started = false;

    const flush = () => {
      if (!sb || sb.updating || queue.length === 0) return;
      try {
        sb.appendBuffer(queue.shift() as ArrayBuffer);
      } catch {
        queue.shift();
      }
    };

    // Set up the MediaSource only once the socket is open, so the request we
    // send from `sourceopen` can never race an unopened socket.
    ws.onopen = () => {
      if (ws !== this.ws) return;
      const ms = new MS();
      ms.addEventListener(
        "sourceopen",
        () => {
          if (this.video.src) URL.revokeObjectURL(this.video.src);
          const codecs = MSE_CODECS.filter((c) =>
            MS.isTypeSupported(`video/mp4; codecs="${c}"`),
          ).join();
          ws.send(JSON.stringify({ type: "mse", value: codecs }));
        },
        { once: true },
      );

      if ((window as unknown as { ManagedMediaSource?: unknown }).ManagedMediaSource) {
        (this.video as unknown as { disableRemotePlayback: boolean }).disableRemotePlayback = true;
        this.video.srcObject = ms;
      } else {
        this.video.src = URL.createObjectURL(ms);
      }
      void this.video.play().catch(() => undefined);

      ws.onmessage = (ev) => {
        if (typeof ev.data === "string") {
          let msg: { type: string; value: string };
          try {
            msg = JSON.parse(ev.data);
          } catch {
            return;
          }
          if (msg.type === "mse") {
            try {
              sb = ms.addSourceBuffer(msg.value);
              sb.mode = "segments";
              sb.addEventListener("updateend", () => {
                flush();
                if (sb && !sb.updating && sb.buffered.length) {
                  const end = sb.buffered.end(sb.buffered.length - 1);
                  const start0 = sb.buffered.start(0);
                  const liveStart = end - 5; // hold ~5s, drop the rest for low latency
                  if (liveStart > start0) {
                    try {
                      sb.remove(start0, liveStart);
                    } catch {
                      /* ignore */
                    }
                  }
                  if (this.video.currentTime < liveStart) {
                    this.video.currentTime = end - 0.4;
                  }
                }
              });
            } catch {
              this.onState("error");
            }
          } else if (msg.type === "error") {
            this.onState("error");
          }
          return;
        }
        queue.push(ev.data as ArrayBuffer);
        flush();
        if (!started) {
          started = true;
          this.markPlaying();
          void this.video.play().catch(() => undefined);
        }
      };
    };

    ws.onerror = () => {
      if (ws !== this.ws) return;
      if (!started) this.onState("error");
    };
    ws.onclose = () => {
      if (ws !== this.ws) return;
      if (!this.destroyed && !started) this.onState("error");
    };
    this.timer = window.setTimeout(() => {
      if (!started && !this.destroyed) this.onState("error");
    }, 9000);
  }
}
