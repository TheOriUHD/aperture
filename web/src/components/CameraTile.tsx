import { Maximize2 } from "lucide-react";
import { LiveVideo } from "./LiveVideo";
import type { Camera } from "../lib/types";

export function CameraTile({ camera, onOpen }: { camera: Camera; onOpen?: (c: Camera) => void }) {
  const recording = camera.enabled && camera.recordMode !== "never";

  return (
    <div
      className={`group relative aspect-video rounded-lg overflow-hidden bg-ink-750 border border-line ${
        onOpen ? "cursor-pointer hover:border-line-strong" : ""
      }`}
      onClick={() => onOpen?.(camera)}
    >
      <LiveVideo camera={camera} className="absolute inset-0" />

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

      {/* Hover expand hint */}
      {onOpen && (
        <div className="absolute top-2.5 right-2.5 opacity-0 group-hover:opacity-100 transition-opacity">
          <span className="h-7 w-7 grid place-items-center rounded-md bg-black/55 text-fg-muted">
            <Maximize2 className="h-3.5 w-3.5" />
          </span>
        </div>
      )}

      {/* Name (text shadow, no gradient band) */}
      <div className="absolute bottom-2.5 left-3 right-3 flex items-center justify-between">
        <span
          className="text-[13px] font-medium text-white"
          style={{ textShadow: "0 1px 3px rgba(0,0,0,0.85)" }}
        >
          {camera.name}
        </span>
      </div>
    </div>
  );
}
