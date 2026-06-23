// Self-contained live player engine for go2rtc's WebSocket API, proxied through
// nvrd. Tries WebRTC first (lowest latency), then falls back to MSE/fMP4. No
// third-party code — we speak the documented go2rtc message protocol directly:
//   client -> {type:"webrtc/offer"|"webrtc/candidate"|"mse", value}
//   server -> {type:"webrtc/answer"|"webrtc/candidate"|"mse", value}

export type LiveState = "connecting" | "playing" | "error";

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

  constructor(
    private video: HTMLVideoElement,
    private url: string,
    private onState: (s: LiveState) => void,
  ) {}

  start() {
    this.onState("connecting");
    this.connectWebRTC();
  }

  destroy() {
    this.destroyed = true;
    this.teardown();
  }

  private teardown() {
    this.clearTimer();
    try {
      this.ws?.close();
    } catch {
      /* ignore */
    }
    try {
      this.pc?.close();
    } catch {
      /* ignore */
    }
    this.ws = undefined;
    this.pc = undefined;
  }

  private clearTimer() {
    if (this.timer) window.clearTimeout(this.timer);
    this.timer = undefined;
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
      stream.addTrack(ev.track);
      this.video.srcObject = stream;
      void this.video.play().catch(() => undefined);
    };
    pc.onconnectionstatechange = () => {
      if (pc.connectionState === "connected") {
        this.clearTimer();
        this.onState("playing");
      } else if (pc.connectionState === "failed") {
        this.fail();
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
      try {
        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
        ws.send(JSON.stringify({ type: "webrtc/offer", value: offer.sdp }));
      } catch {
        this.fail();
      }
    };
    ws.onmessage = (ev) => {
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
    ws.onerror = () => this.fail();

    this.timer = window.setTimeout(() => {
      if (pc.connectionState !== "connected") this.fail();
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
          this.clearTimer();
          this.onState("playing");
          void this.video.play().catch(() => undefined);
        }
      };
    };

    ws.onerror = () => {
      if (!started) this.onState("error");
    };
    ws.onclose = () => {
      if (!this.destroyed && !started) this.onState("error");
    };
    this.timer = window.setTimeout(() => {
      if (!started && !this.destroyed) this.onState("error");
    }, 9000);
  }
}
