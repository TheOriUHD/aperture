import { useRef } from "react";
import { Minus, Plus } from "lucide-react";

const MAX = 8; // 800%
const STEP = 0.5;
const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));
const round2 = (v: number) => Math.round(v * 100) / 100;

// Bottom-right viewer zoom for the single player. One fixed-width control: the
// −/%/+ row never moves or resizes; the navigator only extends upward out of the
// same width when zoomed past 100%. Panning happens only by dragging the box —
// the player itself is never draggable. Clicking the % resets to 100%.
export function ViewerZoom({
  zoom,
  pan,
  onZoom,
  onPan,
}: {
  zoom: number;
  pan: { x: number; y: number }; // viewport CENTRE as a fraction of the frame
  onZoom: (z: number) => void;
  onPan: (p: { x: number; y: number }) => void;
}) {
  const navRef = useRef<HTMLDivElement>(null);
  const dragging = useRef(false);

  const frac = 1 / zoom; // viewport rect size as a fraction of the frame
  const lo = frac / 2;
  const hi = 1 - frac / 2;
  const cx = clamp(pan.x, lo, hi);
  const cy = clamp(pan.y, lo, hi);

  function moveTo(e: React.PointerEvent) {
    const el = navRef.current;
    if (!el) return;
    const b = el.getBoundingClientRect();
    onPan({
      x: clamp((e.clientX - b.left) / b.width, lo, hi),
      y: clamp((e.clientY - b.top) / b.height, lo, hi),
    });
  }

  return (
    <div className="pointer-events-auto absolute bottom-3 right-3 z-20 flex w-32 flex-col gap-1.5">
      {zoom > 1 && (
        <div
          ref={navRef}
          onPointerDown={(e) => {
            dragging.current = true;
            e.currentTarget.setPointerCapture(e.pointerId);
            moveTo(e);
          }}
          onPointerMove={(e) => dragging.current && moveTo(e)}
          onPointerUp={() => (dragging.current = false)}
          onPointerCancel={() => (dragging.current = false)}
          className="osd-panel relative aspect-video w-full cursor-move touch-none overflow-hidden"
          title="Drag to move the zoomed area"
        >
          <div
            className="absolute rounded-[2px] border border-brand bg-brand/25"
            style={{
              left: `${(cx - frac / 2) * 100}%`,
              top: `${(cy - frac / 2) * 100}%`,
              width: `${frac * 100}%`,
              height: `${frac * 100}%`,
            }}
          />
        </div>
      )}

      <div className="osd-chip w-full justify-between gap-0 px-0.5">
        <button
          onClick={() => onZoom(round2(Math.max(1, zoom - STEP)))}
          disabled={zoom <= 1}
          className="grid h-7 w-7 place-items-center rounded text-white/85 transition-colors hover:bg-white/10 disabled:opacity-30 disabled:hover:bg-transparent"
          aria-label="Zoom out"
        >
          <Minus className="h-3.5 w-3.5" />
        </button>
        <button
          onClick={() => onZoom(1)}
          className="h-7 flex-1 rounded text-center text-xs font-medium tabular-nums text-white transition-colors hover:text-brand"
          title="Reset to 100%"
        >
          {Math.round(zoom * 100)}%
        </button>
        <button
          onClick={() => onZoom(round2(Math.min(MAX, zoom + STEP)))}
          disabled={zoom >= MAX}
          className="grid h-7 w-7 place-items-center rounded text-white/85 transition-colors hover:bg-white/10 disabled:opacity-30 disabled:hover:bg-transparent"
          aria-label="Zoom in"
        >
          <Plus className="h-3.5 w-3.5" />
        </button>
      </div>
    </div>
  );
}
