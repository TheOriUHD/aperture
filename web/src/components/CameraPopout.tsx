import { useEffect, useRef } from "react";
import { X, Maximize2, Camera as CameraIcon, Clapperboard } from "lucide-react";
import { LiveVideo } from "./LiveVideo";
import type { Camera } from "../lib/types";

export function CameraPopout({
  camera,
  onClose,
  onOpenPlayback,
}: {
  camera: Camera | null;
  onClose: () => void;
  onOpenPlayback: (cameraId: string) => void;
}) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!camera) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [camera, onClose]);

  if (!camera) return null;

  async function snapshot() {
    try {
      const res = await fetch(`/api/cameras/${camera!.id}/snapshot.jpg`, { credentials: "same-origin" });
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${camera!.name}-${new Date().toISOString().replace(/[:.]/g, "-")}.jpg`;
      a.click();
      URL.revokeObjectURL(url);
    } catch {
      /* ignore */
    }
  }

  function toggleFullscreen() {
    const el = containerRef.current;
    if (!document.fullscreenElement) void el?.requestFullscreen?.();
    else void document.exitFullscreen();
  }

  const recording = camera.enabled && camera.recordMode !== "never";

  return (
    <div className="fixed inset-0 z-50 grid place-items-center p-4">
      <div className="absolute inset-0 bg-black/75 animate-fade-in" onClick={onClose} />
      <div
        ref={containerRef}
        className="relative w-[min(94vw,1100px)] card overflow-hidden shadow-pop animate-fade-in"
      >
        <LiveVideo camera={camera} fit="contain" className="aspect-video bg-black" />

        {/* Top overlay: name + close */}
        <div className="absolute top-0 inset-x-0 h-14 bg-gradient-to-b from-black/70 to-transparent pointer-events-none" />
        <div className="absolute top-3 left-4 flex items-center gap-2.5">
          {recording && (
            <span className="pill-rec">
              <span className="h-1.5 w-1.5 rounded-full bg-rec animate-rec-pulse" />
              REC
            </span>
          )}
          <span className="text-sm font-medium text-white drop-shadow">{camera.name}</span>
        </div>
        <button
          onClick={onClose}
          className="absolute top-3 right-3 h-8 w-8 grid place-items-center rounded-md bg-black/55 text-white/90 hover:bg-black/75"
          aria-label="Close"
        >
          <X className="h-4 w-4" />
        </button>

        {/* Bottom controls */}
        <div className="absolute bottom-0 inset-x-0 h-16 bg-gradient-to-t from-black/70 to-transparent pointer-events-none" />
        <div className="absolute bottom-3 inset-x-4 flex items-center gap-2">
          <button
            onClick={() => onOpenPlayback(camera.id)}
            className="btn-primary h-9"
          >
            <Clapperboard className="h-4 w-4" />
            View recordings
          </button>
          <div className="ml-auto flex items-center gap-1.5">
            <button
              onClick={snapshot}
              className="h-9 w-9 grid place-items-center rounded-md bg-black/55 text-white/90 hover:bg-black/75"
              aria-label="Snapshot"
            >
              <CameraIcon className="h-4 w-4" />
            </button>
            <button
              onClick={toggleFullscreen}
              className="h-9 w-9 grid place-items-center rounded-md bg-black/55 text-white/90 hover:bg-black/75"
              aria-label="Fullscreen"
            >
              <Maximize2 className="h-4 w-4" />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
