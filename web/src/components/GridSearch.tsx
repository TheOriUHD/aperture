import { useRef, useState } from "react";
import { X, Check, Eraser } from "lucide-react";
import { MOTION_GRID_W, MOTION_GRID_H, type MotionEvent } from "../lib/types";

export interface Region {
  x0: number;
  y0: number;
  x1: number;
  y1: number;
} // normalized [0..1]

const clamp01 = (v: number) => Math.max(0, Math.min(1, v));

// regionMatch: does any of an event's motion cells fall inside the drawn region?
export function regionMatch(cells: number[] | undefined, region: Region | null): boolean {
  if (!region) return true;
  if (!cells?.length) return false;
  for (const idx of cells) {
    const cx = (idx % MOTION_GRID_W) / MOTION_GRID_W;
    const cy = Math.floor(idx / MOTION_GRID_W) / MOTION_GRID_H;
    const cx1 = cx + 1 / MOTION_GRID_W;
    const cy1 = cy + 1 / MOTION_GRID_H;
    if (cx < region.x1 && cx1 > region.x0 && cy < region.y1 && cy1 > region.y0) return true;
  }
  return false;
}

export function filterByRegion(events: MotionEvent[], region: Region | null): MotionEvent[] {
  if (!region) return events;
  return events.filter((e) => regionMatch(e.motionCells, region));
}

// GridSearchOverlay: drag a rectangle over the camera view to define a motion
// search area (UniFi "Grid Search"). Shows a faint 32×18 grid for reference.
export function GridSearchOverlay({
  region,
  onApply,
  onClose,
}: {
  region: Region | null;
  onApply: (r: Region | null) => void;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [draft, setDraft] = useState<Region | null>(region);
  const drag = useRef<{ x: number; y: number } | null>(null);

  const pt = (e: React.PointerEvent) => {
    const el = ref.current!;
    const b = el.getBoundingClientRect();
    return { x: clamp01((e.clientX - b.left) / b.width), y: clamp01((e.clientY - b.top) / b.height) };
  };
  const onDown = (e: React.PointerEvent) => {
    e.currentTarget.setPointerCapture(e.pointerId);
    const p = pt(e);
    drag.current = p;
    setDraft({ x0: p.x, y0: p.y, x1: p.x, y1: p.y });
  };
  const onMove = (e: React.PointerEvent) => {
    if (!drag.current) return;
    const p = pt(e);
    const a = drag.current;
    setDraft({ x0: Math.min(a.x, p.x), y0: Math.min(a.y, p.y), x1: Math.max(a.x, p.x), y1: Math.max(a.y, p.y) });
  };
  const onUp = () => {
    drag.current = null;
    if (draft && (draft.x1 - draft.x0 < 0.02 || draft.y1 - draft.y0 < 0.02)) setDraft(null);
  };

  const cols = Array.from({ length: MOTION_GRID_W - 1 }, (_, i) => ((i + 1) / MOTION_GRID_W) * 100);
  const rows = Array.from({ length: MOTION_GRID_H - 1 }, (_, i) => ((i + 1) / MOTION_GRID_H) * 100);

  return (
    <div className="absolute inset-0 z-20">
      <div
        ref={ref}
        onPointerDown={onDown}
        onPointerMove={onMove}
        onPointerUp={onUp}
        className="absolute inset-0 cursor-crosshair touch-none bg-black/30"
      >
        {/* faint reference grid */}
        <svg className="pointer-events-none absolute inset-0 h-full w-full" preserveAspectRatio="none" viewBox="0 0 100 100">
          {cols.map((x) => (
            <line key={`c${x}`} x1={x} y1={0} x2={x} y2={100} stroke="rgba(255,255,255,0.08)" strokeWidth={0.2} />
          ))}
          {rows.map((y) => (
            <line key={`r${y}`} x1={0} y1={y} x2={100} y2={y} stroke="rgba(255,255,255,0.08)" strokeWidth={0.2} />
          ))}
        </svg>
        {draft && (
          <div
            className="absolute border-2 border-brand bg-brand/20"
            style={{
              left: `${draft.x0 * 100}%`,
              top: `${draft.y0 * 100}%`,
              width: `${(draft.x1 - draft.x0) * 100}%`,
              height: `${(draft.y1 - draft.y0) * 100}%`,
            }}
          />
        )}
        {!draft && (
          <div className="pointer-events-none absolute inset-0 grid place-items-center">
            <span className="rounded-full bg-black/60 px-3 py-1.5 text-xs text-white">
              Drag to draw a motion search area
            </span>
          </div>
        )}
      </div>

      <div className="absolute bottom-3 left-1/2 flex -translate-x-1/2 items-center gap-2 rounded-full bg-ink-850/95 px-2 py-1.5 shadow-pop">
        <span className="px-1.5 text-xs text-fg-muted">Grid Search</span>
        <button
          onClick={() => setDraft(null)}
          className="btn-icon h-8 w-8"
          title="Clear area"
        >
          <Eraser className="h-4 w-4" />
        </button>
        <button onClick={onClose} className="btn-icon h-8 w-8" title="Cancel">
          <X className="h-4 w-4" />
        </button>
        <button
          onClick={() => {
            onApply(draft);
            onClose();
          }}
          className="btn-primary h-8"
        >
          <Check className="h-4 w-4" /> Apply
        </button>
      </div>
    </div>
  );
}
