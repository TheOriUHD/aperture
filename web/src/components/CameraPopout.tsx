import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { X, Maximize, Camera as CameraIcon, Clapperboard, ScanEye } from "lucide-react";
import { useObjectBoxes } from "./ObjectBox";
import { LiveVideo } from "./LiveVideo";
import { LiveConditions } from "./ConditionsOverlay";
import { LiveObjectsOverlay } from "./LiveObjectsOverlay";
import { ExtensionSlot } from "../ext/ExtensionSlot";
import { fsVideoStyle, FullscreenChrome } from "./fullscreen";
import { useScrollLock } from "../hooks/useScrollLock";
import { useMediaQuery } from "../hooks/useMediaQuery";
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
  const videoRef = useRef<HTMLVideoElement>(null);
  const [boxesOn, setBoxesOn] = useObjectBoxes();
  const isDesktop = useMediaQuery("(min-width: 1024px)");
  const [fs, setFs] = useState(false);
  // Opened from a fullscreen camera wall, the pop-out must render inside it:
  // nothing outside the fullscreen element is shown. Chosen when it opens, so
  // its own fullscreen button doesn't move it.
  const [host, setHost] = useState<Element | null>(null);
  useEffect(() => {
    if (camera) setHost(document.fullscreenElement ?? document.body);
  }, [camera?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  // The window takes the camera's own shape, so a 180° panorama opens wide with
  // no black bars. The shape seen last time sizes it at once; the video's
  // actual size corrects it as soon as it is known.
  const [aspect, setAspect] = useState(16 / 9);
  useEffect(() => {
    if (!camera) return;
    setAspect(readAspect(camera.id));
    let v: HTMLVideoElement | null = null;
    const measure = () => {
      const el = videoRef.current;
      if (el && el.videoWidth > 0 && el.videoHeight > 0) {
        const a = el.videoWidth / el.videoHeight;
        setAspect(a);
        saveAspect(camera.id, a);
      }
    };
    // The video element appears once the stream connects.
    const t = window.setInterval(() => {
      if (videoRef.current && videoRef.current !== v) {
        v?.removeEventListener("loadedmetadata", measure);
        v?.removeEventListener("resize", measure);
        v = videoRef.current;
        v.addEventListener("loadedmetadata", measure);
        v.addEventListener("resize", measure);
        measure();
      }
    }, 250);
    return () => {
      window.clearInterval(t);
      v?.removeEventListener("loadedmetadata", measure);
      v?.removeEventListener("resize", measure);
    };
  }, [camera?.id]); // eslint-disable-line react-hooks/exhaustive-deps
  useScrollLock(!!camera);

  useEffect(() => {
    if (!camera) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && (fs ? setFs(false) : onClose());
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [camera, onClose, fs]);

  // Drop out of fullscreen whenever the pop-out target changes or closes.
  useEffect(() => {
    setFs(false);
  }, [camera?.id]);

  if (!camera) return null;

  async function snapshot() {
    try {
      const res = await fetch(`/api/v1/cameras/${camera!.id}/snapshot.jpg`, { credentials: "same-origin" });
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
    // Mobile: rotate + fill (iPhone blocks element fullscreen). Desktop: native.
    if (!isDesktop) {
      setFs((f) => !f);
      return;
    }
    const el = containerRef.current;
    if (!document.fullscreenElement) void el?.requestFullscreen?.();
    else void document.exitFullscreen();
  }

  const recording = camera.recordingNow;
  const mobileFs = fs && !isDesktop;

  return createPortal(
    <div className="fixed inset-0 z-50 grid place-items-center p-4">
      <div className="absolute inset-0 bg-black/75 animate-fade-in" onClick={onClose} />
      {/* The player frame: picture on black with the control bar underneath,
          inside the same frame. Sized so picture + bar always fit the viewport
          height; in native fullscreen the picture takes all the height the bar
          leaves. */}
      <div
        ref={containerRef}
        className="frame relative flex flex-col overflow-hidden border border-line shadow-pop animate-fade-in transition-[width] duration-200 [&:fullscreen]:!w-auto [&:fullscreen]:rounded-none [&:fullscreen]:border-0"
        style={{
          // As wide as the picture allows: the viewport, a comfortable maximum
          // (wider for panoramas), and the height left for picture + control bar.
          width: `min(94vw, ${Math.round(1100 * Math.max(1, aspect / (16 / 9)))}px, calc((100dvh - 8rem) * ${aspect.toFixed(4)}))`,
        }}
      >
        <div
          className="relative bg-black [:fullscreen>&]:!aspect-auto [:fullscreen>&]:min-h-0 [:fullscreen>&]:flex-1"
          style={{ aspectRatio: String(aspect) }}
        >
          <LiveVideo camera={camera} fit="contain" videoRef={videoRef} fsStyle={mobileFs ? fsVideoStyle : undefined} />
          <LiveObjectsOverlay cameraId={camera.id} videoRef={videoRef} fit="contain" outline={false} />
          <LiveConditions variant="full" className="top-3 right-14" cameraId={camera.id} />
          <button onClick={onClose} className="osd-btn absolute right-3 top-3" aria-label="Close" title="Close">
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* control bar: what you are watching on the left, view tools centred,
            the way into this camera's recordings on the right */}
        <div className="grid h-12 shrink-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-3 px-3">
          <div className="flex min-w-0 items-center gap-2.5">
            <span className="pill-blue h-6 shrink-0 gap-1.5 px-2 font-semibold tracking-wide">
              <span className="h-1.5 w-1.5 rounded-full bg-brand" />
              LIVE
            </span>
            <span className="truncate text-[13px] font-semibold text-fg">{camera.name}</span>
            {recording && (
              <span className="hidden shrink-0 items-center gap-1 text-[11px] font-medium text-fg-muted sm:flex">
                <span className="h-1.5 w-1.5 rounded-full bg-rec animate-rec-pulse" />
                REC
              </span>
            )}
          </div>
          <div className="flex items-center gap-0.5">
            <button
              onClick={() => setBoxesOn(!boxesOn)}
              className={`btn-icon ${boxesOn ? "text-brand" : ""}`}
              aria-pressed={boxesOn}
              aria-label={boxesOn ? "Hide object boxes" : "Show object boxes"}
              title={boxesOn ? "Hide object boxes" : "Show object boxes"}
            >
              <ScanEye className="h-4 w-4" />
            </button>
            <button onClick={snapshot} className="btn-icon" aria-label="Snapshot" title="Snapshot">
              <CameraIcon className="h-4 w-4" />
            </button>
            <button onClick={toggleFullscreen} className="btn-icon" aria-label="Fullscreen" title="Fullscreen">
              <Maximize className="h-4 w-4" />
            </button>
          </div>
          <div className="flex justify-end">
            <button
              onClick={() => onOpenPlayback(camera.id)}
              className="btn-outline px-2.5 sm:px-3.5"
              aria-label="View recordings"
              title="View recordings"
            >
              <Clapperboard className="h-4 w-4" />
              <span className="hidden sm:inline">View recordings</span>
            </button>
          </div>
        </div>

        {/* Extension camera-detail panels flow below the player (nothing when none). */}
        <ExtensionSlot slot="camera-detail" cameraId={camera.id} />
      </div>
      {mobileFs && <FullscreenChrome onExit={() => setFs(false)} />}
    </div>,
    host ?? document.body,
  );
}

// The last known shape of each camera's picture (per browser).
function readAspect(id: string): number {
  try {
    const v = Number(localStorage.getItem(`aperture.aspect.${id}`));
    if (v > 0.2 && v < 10) return v;
  } catch {
    /* no storage */
  }
  return 16 / 9;
}

function saveAspect(id: string, a: number) {
  try {
    localStorage.setItem(`aperture.aspect.${id}`, a.toFixed(4));
  } catch {
    /* no storage */
  }
}
