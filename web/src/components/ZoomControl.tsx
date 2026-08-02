import { ZoomIn, ZoomOut } from "lucide-react";

// Touch-friendly timeline zoom: two large (44px) +/- buttons stacked in a pill.
// Replaces the old thin vertical slider, which was almost impossible to hit on a
// phone (a millimetre off and the press fell through to the timeline scrub).
// Higher value = zoomed in.
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
  return (
    <div
      // Claim the touch so an on-control press never falls through to the
      // full-bleed timeline scrubber underneath.
      onPointerDown={(e) => e.stopPropagation()}
      className={`flex flex-col items-center rounded-full bg-ink-700/95 backdrop-blur-sm shadow-pop ${className}`}
    >
      <button
        className="grid h-11 w-11 place-items-center rounded-full text-fg-muted active:text-fg disabled:opacity-40"
        aria-label="Zoom in"
        disabled={value >= max}
        onClick={() => onChange(Math.min(max, value + 1))}
      >
        <ZoomIn className="h-5 w-5" />
      </button>
      <div className="h-px w-5 bg-line" />
      <button
        className="grid h-11 w-11 place-items-center rounded-full text-fg-muted active:text-fg disabled:opacity-40"
        aria-label="Zoom out"
        disabled={value <= 0}
        onClick={() => onChange(Math.max(0, value - 1))}
      >
        <ZoomOut className="h-5 w-5" />
      </button>
    </div>
  );
}
