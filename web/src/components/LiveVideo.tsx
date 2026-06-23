import { useEffect, useRef, useState } from "react";
import { VideoOff } from "lucide-react";
import { api } from "../lib/api";
import { LiveStream, type LiveState } from "../lib/livestream";
import type { Camera, StreamToken } from "../lib/types";

// Bare live player: a <video> wired to the WebRTC/MSE engine. While connecting
// it shows a blurred snapshot of the camera (its last/current frame) with an
// animated dot loader, instead of a black box.
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
  const [snapOk, setSnapOk] = useState(true);

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
    <div className={`relative overflow-hidden ${className}`}>
      <video
        ref={videoRef}
        muted={muted}
        autoPlay
        playsInline
        className={`h-full w-full bg-black ${fit === "cover" ? "object-cover" : "object-contain"}`}
      />

      {state !== "playing" && (
        <div className="absolute inset-0">
          {camera.enabled && snapOk ? (
            <img
              src={`/api/cameras/${camera.id}/snapshot.jpg`}
              alt=""
              className="absolute inset-0 h-full w-full object-cover scale-110 blur-xl brightness-[0.45]"
              onError={() => setSnapOk(false)}
            />
          ) : (
            <div className="absolute inset-0 bg-ink-850" />
          )}
          <div className="absolute inset-0 grid place-items-center">
            {state === "connecting" ? (
              <div className="flex items-center gap-1.5">
                <span className="cdot" style={{ animationDelay: "0ms" }} />
                <span className="cdot" style={{ animationDelay: "160ms" }} />
                <span className="cdot" style={{ animationDelay: "320ms" }} />
              </div>
            ) : (
              <div className="flex flex-col items-center gap-1 text-white/70">
                <VideoOff className="h-6 w-6" />
                <span className="text-xs">{camera.enabled ? "No signal" : "Disabled"}</span>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
