// Self-contained live player engine for go2rtc's WebSocket API, proxied through
// nvrd. Tries WebRTC first (lowest latency), then falls back to MSE/fMP4. No
// third-party code — we speak the documented go2rtc message protocol directly.

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

export class LiveStream {
  private ws?: WebSocket;
  private pc?: RTCPeerConnection;
  private sb?: SourceBuffer;
  private queue: ArrayBuffer[] = [];
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
    this.teardownConn();
  }

  private teardownConn() {
    if (this.timer) window.clearTimeout(this.timer);
    this.timer = undefined;
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
    this.sb = undefined;
    this.queue = [];
  }

  private fail() {
    if (this.destroyed) return;
    if (this.mode === "webrtc") {
      this.teardownConn();
      this.mode = "mse";
      this.connectMSE();
    } else {
      this.onState("error");
    }
  }

  // --- WebRTC ---------------------------------------------------------------
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
        if (this.timer) window.clearTimeout(this.timer);
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
        ws.send(JSON.stringify({ type: "webrtc", value: offer.sdp }));
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
      if (msg.type === "webrtc") {
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
    }, 6000);
  }

  // --- MSE fallback ---------------------------------------------------------
  private connectMSE() {
    this.onState("connecting");
    const ws = new WebSocket(this.url);
    this.ws = ws;
    ws.binaryType = "arraybuffer";

    const ms = new MediaSource();
    this.video.src = URL.createObjectURL(ms);
    let started = false;

    ms.addEventListener(
      "sourceopen",
      () => {
        const codecs = MSE_CODECS.filter((c) =>
          MediaSource.isTypeSupported(`video/mp4; codecs="${c}"`),
        );
        ws.send(JSON.stringify({ type: "mse", value: codecs.join(",") }));
      },
      { once: true },
    );

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
            const sb = ms.addSourceBuffer(msg.value);
            sb.mode = "segments";
            sb.addEventListener("updateend", () => this.flush());
            this.sb = sb;
          } catch {
            this.onState("error");
          }
        } else if (msg.type === "error") {
          this.onState("error");
        }
        return;
      }
      this.queue.push(ev.data as ArrayBuffer);
      this.flush();
      if (!started) {
        started = true;
        if (this.timer) window.clearTimeout(this.timer);
        this.onState("playing");
        void this.video.play().catch(() => undefined);
      }
    };
    ws.onerror = () => {
      if (!started) this.onState("error");
    };
    ws.onclose = () => {
      if (!this.destroyed && !started) this.onState("error");
    };
  }

  private flush() {
    const sb = this.sb;
    if (!sb || sb.updating || this.queue.length === 0) return;
    // Keep the buffer bounded so latency and memory stay in check.
    try {
      if (sb.buffered.length > 0) {
        const end = sb.buffered.end(sb.buffered.length - 1);
        const start = sb.buffered.start(0);
        if (end - start > 90 && !sb.updating) {
          sb.remove(start, end - 60);
          return;
        }
      }
      sb.appendBuffer(this.queue.shift() as ArrayBuffer);
    } catch {
      // QuotaExceededError or invalid state: drop a chunk and continue.
      this.queue.shift();
    }
  }
}
