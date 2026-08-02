import { useEffect, useRef } from "react";
import { X, Maximize2, Camera as CameraIcon, Clapperboard } from "lucide-react";
import { LiveVideo } from "./LiveVideo";
import { LiveConditions } from "./ConditionsOverlay";
import { useScrollLock } from "../hooks/useScrollLock";
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
  useScrollLock(!!camera);

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
        <div className="relative aspect-video bg-black">
          <LiveVideo camera={camera} fit="contain" />
          <LiveConditions variant="full" className="top-3 right-14" />
        </div>

        {/* Top overlay: subtle name pill + close */}
        <div className="absolute top-3 left-3 flex items-center gap-2 rounded-md bg-black/35 px-2.5 py-1 backdrop-blur-sm">
          {recording && (
            <span className="flex items-center gap-1 text-[11px] font-medium text-[#ffd6d7]">
              <span className="h-1.5 w-1.5 rounded-full bg-rec animate-rec-pulse" />
              REC
            </span>
          )}
          <span className="text-xs font-medium text-white">{camera.name}</span>
        </div>
        <button
          onClick={onClose}
          className="absolute top-3 right-3 h-8 w-8 grid place-items-center rounded-md bg-black/45 text-white/90 hover:bg-black/70 backdrop-blur-sm"
          aria-label="Close"
        >
          <X className="h-4 w-4" />
        </button>

        {/* Bottom controls (self-contained pills, no gradient band) */}
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
