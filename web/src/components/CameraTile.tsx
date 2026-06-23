import { useEffect, useRef, useState } from "react";
import { Maximize2, VideoOff, Loader2, Camera as CameraIcon } from "lucide-react";
import { api } from "../lib/api";
import { LiveStream, type LiveState } from "../lib/livestream";
import type { Camera, StreamToken } from "../lib/types";

export function CameraTile({ camera, onExpand }: { camera: Camera; onExpand?: () => void }) {
  const videoRef = useRef<HTMLVideoElement>(null);
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
  }, [camera.id, camera.enabled]);

  const recording = camera.enabled && camera.recordMode !== "never";

  return (
    <div className="group relative aspect-video rounded-lg overflow-hidden bg-ink-750 border border-line">
      <video
        ref={videoRef}
        muted
        autoPlay
        playsInline
        className="h-full w-full object-cover bg-black"
      />

      {state !== "playing" && (
        <div className="absolute inset-0 grid place-items-center bg-ink-800/90 text-center px-4">
          {state === "connecting" ? (
            <div className="flex items-center gap-2 text-fg-muted text-sm">
              <Loader2 className="h-4 w-4 animate-spin text-brand" />
              Connecting…
            </div>
          ) : (
            <div className="flex flex-col items-center gap-1.5 text-fg-faint">
              <VideoOff className="h-6 w-6" />
              <span className="text-sm">{camera.enabled ? "No signal" : "Disabled"}</span>
            </div>
          )}
        </div>
      )}

      {/* Top-left status pill */}
      <div className="absolute top-2.5 left-2.5 flex items-center gap-2">
        {recording ? (
          <span className="pill-rec">
            <span className="h-1.5 w-1.5 rounded-full bg-rec animate-rec-pulse" />
            REC
          </span>
        ) : (
          <span className="pill-offline">OFF</span>
        )}
      </div>

      {/* Hover controls */}
      <div className="absolute top-2.5 right-2.5 opacity-0 group-hover:opacity-100 transition-opacity flex gap-1.5">
        <button
          className="h-7 w-7 grid place-items-center rounded-md bg-black/55 text-fg-muted hover:text-fg"
          aria-label="Snapshot"
        >
          <CameraIcon className="h-3.5 w-3.5" />
        </button>
        {onExpand && (
          <button
            onClick={onExpand}
            className="h-7 w-7 grid place-items-center rounded-md bg-black/55 text-fg-muted hover:text-fg"
            aria-label="Expand"
          >
            <Maximize2 className="h-3.5 w-3.5" />
          </button>
        )}
      </div>

      {/* Bottom gradient + name */}
      <div className="absolute inset-x-0 bottom-0 h-14 bg-gradient-to-t from-black/70 to-transparent pointer-events-none" />
      <div className="absolute bottom-2.5 left-3 right-3 flex items-center justify-between">
        <span className="text-[13px] font-medium text-white drop-shadow">{camera.name}</span>
      </div>
    </div>
  );
}
