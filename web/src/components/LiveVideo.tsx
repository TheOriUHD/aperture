import { useEffect, useRef, useState } from "react";
import { VideoOff } from "lucide-react";
import { api } from "../lib/api";
import { LiveStream, type LiveState } from "../lib/livestream";
import type { Camera, StreamToken } from "../lib/types";

type FrameVideo = HTMLVideoElement & {
  requestVideoFrameCallback?: (cb: () => void) => number;
};

// Bare live player: always fills its (relative, sized) parent. A blurred full
// frame + dot loader is shown until the video actually presents a frame, then
// it fades out — so the live image is never a black flash.
export function LiveVideo({
  camera,
  muted = true,
  fit = "cover",
  videoRef: externalRef,
}: {
  camera: Camera;
  muted?: boolean;
  fit?: "cover" | "contain";
  videoRef?: React.RefObject<HTMLVideoElement>;
}) {
  const innerRef = useRef<HTMLVideoElement>(null);
  const videoRef = externalRef ?? innerRef;
  const [state, setState] = useState<LiveState>("connecting");
  const [revealed, setRevealed] = useState(false);
  const [snapOk, setSnapOk] = useState(true);

  // Live stream lifecycle.
  useEffect(() => {
    if (!camera.enabled) {
      setState("error");
      return;
    }
    let stream: LiveStream | undefined;
    let cancelled = false;
    setState("connecting");
    (async () => {
      try {
        const tok = await api.get<StreamToken>(`/api/cameras/${camera.id}/stream-token`);
        if (cancelled || !videoRef.current) return;
        const proto = location.protocol === "https:" ? "wss" : "ws";
        const url = `${proto}://${location.host}${tok.wsUrl}`;
        stream = new LiveStream(videoRef.current, url, (s) => !cancelled && setState(s));
        stream.start();
      } catch {
        if (!cancelled) setState("error");
      }
    })();
    return () => {
      cancelled = true;
      stream?.destroy();
    };
  }, [camera.id, camera.enabled, videoRef]);

  // Reveal only once a real frame is presented (no black flash).
  useEffect(() => {
    setRevealed(false);
    setSnapOk(true);
    const v = videoRef.current as FrameVideo | null;
    if (!v) return;
    let cancelled = false;
    const reveal = () => {
      if (!cancelled) setRevealed(true);
    };
    if (typeof v.requestVideoFrameCallback === "function") {
      v.requestVideoFrameCallback(reveal);
    }
    const onData = () => window.setTimeout(reveal, 80);
    v.addEventListener("loadeddata", onData);
    v.addEventListener("playing", onData);
    return () => {
      cancelled = true;
      v.removeEventListener("loadeddata", onData);
      v.removeEventListener("playing", onData);
    };
  }, [camera.id, videoRef]);

  const loaded = revealed && state === "playing";

  return (
    <div className="absolute inset-0 overflow-hidden bg-black">
      <video
        ref={videoRef}
        muted={muted}
        autoPlay
        playsInline
        draggable={false}
        className={`h-full w-full bg-black ${fit === "cover" ? "object-cover" : "object-contain"}`}
      />

      <div
        className={`absolute inset-0 transition-opacity duration-300 ${
          loaded ? "opacity-0 pointer-events-none" : "opacity-100"
        }`}
      >
        {camera.enabled && snapOk ? (
          <img
            src={`/api/cameras/${camera.id}/snapshot.jpg`}
            alt=""
            draggable={false}
            className="absolute inset-0 h-full w-full object-cover scale-105 blur-2xl brightness-[0.7]"
            onError={() => setSnapOk(false)}
          />
        ) : (
          <div className="absolute inset-0 bg-ink-850" />
        )}
        <div className="absolute inset-0 grid place-items-center">
          {state === "error" ? (
            <div className="flex flex-col items-center gap-1 text-white/70">
              <VideoOff className="h-6 w-6" />
              <span className="text-xs">{camera.enabled ? "No signal" : "Disabled"}</span>
            </div>
          ) : (
            <div className="flex items-center gap-1.5">
              <span className="cdot" style={{ animationDelay: "0ms" }} />
              <span className="cdot" style={{ animationDelay: "160ms" }} />
              <span className="cdot" style={{ animationDelay: "320ms" }} />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
