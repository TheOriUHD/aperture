import { ZoomIn, ZoomOut } from "lucide-react";

// Touch-friendly timeline zoom: two large 48px +/- buttons in a pill. It
// swallows every pointer/click event so a press near (or on) it can never fall
// through to the full-bleed timeline scrubber underneath — and the buttons are
// intentionally NOT `disabled`, because iOS treats a disabled button as
// pointer-events:none and lets the tap pass straight through. Higher value =
// zoomed in.
export function ZoomControl({
  value,
  max,
  onChange,
  className = "",
}: {
  value: number;
  max: number;
  onChange: (v: number) => void;
  className?: string;
}) {
  const swallow = (e: { stopPropagation: () => void }) => e.stopPropagation();
  return (
    <div
      onPointerDown={swallow}
      onPointerUp={swallow}
      onClick={swallow}
      className={`flex touch-none flex-col items-center rounded-lg border border-line bg-ink-800/95 p-0.5 shadow-pop backdrop-blur-sm ${className}`}
    >
      <button
        className={`grid h-12 w-12 place-items-center rounded-md text-fg-muted active:bg-white/[0.08] ${value >= max ? "opacity-40" : ""}`}
        aria-label="Zoom in"
        onClick={(e) => {
          e.stopPropagation();
          if (value < max) onChange(value + 1);
        }}
      >
        <ZoomIn className="h-5 w-5" />
      </button>
      <div className="h-px w-6 bg-line-strong" />
      <button
        className={`grid h-12 w-12 place-items-center rounded-md text-fg-muted active:bg-white/[0.08] ${value <= 0 ? "opacity-40" : ""}`}
        aria-label="Zoom out"
        onClick={(e) => {
          e.stopPropagation();
          if (value > 0) onChange(value - 1);
        }}
      >
        <ZoomOut className="h-5 w-5" />
      </button>
    </div>
  );
}
