import { useEffect, useRef, useState } from "react";
import { Loader2, VideoOff } from "lucide-react";
import { api } from "../lib/api";
import { LiveStream, type LiveState } from "../lib/livestream";
import type { Camera, StreamToken } from "../lib/types";

// Bare live player: a <video> wired to the WebRTC/MSE engine, with a connecting
// / no-signal overlay. Composed by tiles, the pop-out, and Playback live mode.
export function LiveVideo({
  camera,
  className = "",
  muted = true,
  fit = "cover",
  videoRef: externalRef,
}: {
  camera: Camera;
  className?: string;
  muted?: boolean;
  fit?: "cover" | "contain";
  videoRef?: React.RefObject<HTMLVideoElement>;
}) {
  const innerRef = useRef<HTMLVideoElement>(null);
  const videoRef = externalRef ?? innerRef;
  const [state, setState] = useState<LiveState>("connecting");

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

  return (
    <div className={`relative ${className}`}>
      <video
        ref={videoRef}
        muted={muted}
        autoPlay
        playsInline
        className={`h-full w-full bg-black ${fit === "cover" ? "object-cover" : "object-contain"}`}
      />
      {state !== "playing" && (
        <div className="absolute inset-0 grid place-items-center bg-ink-800/90">
          {state === "connecting" ? (
            <Loader2 className="h-5 w-5 animate-spin text-brand" />
          ) : (
            <div className="flex flex-col items-center gap-1 text-fg-faint">
              <VideoOff className="h-6 w-6" />
              <span className="text-xs">{camera.enabled ? "No signal" : "Disabled"}</span>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
