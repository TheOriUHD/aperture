import { Maximize2, GripVertical } from "lucide-react";
import { LiveVideo } from "./LiveVideo";
import { LiveConditions } from "./ConditionsOverlay";
import type { Camera } from "../lib/types";

export function CameraTile({
  camera,
  onOpen,
  draggable,
  isDragging,
  isOver,
  onDragStart,
  onDragOver,
  onDrop,
  onDragEnd,
}: {
  camera: Camera;
  onOpen?: (c: Camera) => void;
  draggable?: boolean;
  isDragging?: boolean;
  isOver?: boolean;
  onDragStart?: () => void;
  onDragOver?: () => void;
  onDrop?: () => void;
  onDragEnd?: () => void;
}) {
  const recording = camera.enabled && camera.recordMode !== "never";

  return (
    <div
      draggable={draggable}
      onDragStart={
        onDragStart
          ? (e) => {
              // Required for the drag to initiate in Chromium/Firefox.
              e.dataTransfer.effectAllowed = "move";
              e.dataTransfer.setData("text/plain", camera.id);
              onDragStart();
            }
          : undefined
      }
      onDragOver={
        onDragOver
          ? (e) => {
              e.preventDefault();
              onDragOver();
            }
          : undefined
      }
      onDrop={
        onDrop
          ? (e) => {
              e.preventDefault();
              onDrop();
            }
          : undefined
      }
      onDragEnd={onDragEnd}
      className={`group relative aspect-video rounded-lg overflow-hidden bg-ink-750 border transition-[opacity,box-shadow] ${
        isOver ? "border-brand ring-1 ring-brand" : "border-line"
      } ${isDragging ? "opacity-40" : ""} ${draggable ? "cursor-grab active:cursor-grabbing" : ""} ${
        onOpen ? "hover:border-line-strong" : ""
      }`}
      onClick={() => onOpen?.(camera)}
    >
      <LiveVideo camera={camera} />

      {/* Drag-to-reorder affordance (admin, desktop) */}
      {draggable && (
        <div className="pointer-events-none absolute top-2.5 left-1/2 -translate-x-1/2 opacity-0 group-hover:opacity-100 transition-opacity">
          <span className="h-6 w-6 grid place-items-center rounded-md bg-black/55 text-white/70">
            <GripVertical className="h-3.5 w-3.5" />
          </span>
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

      {/* Hover expand hint */}
      {onOpen && (
        <div className="absolute top-2.5 right-2.5 opacity-0 group-hover:opacity-100 transition-opacity">
          <span className="h-7 w-7 grid place-items-center rounded-md bg-black/55 text-fg-muted">
            <Maximize2 className="h-3.5 w-3.5" />
          </span>
        </div>
      )}

      {/* Conditions readout (bottom-right, opposite the name) */}
      <LiveConditions variant="compact" className="bottom-2.5 right-3" />

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
