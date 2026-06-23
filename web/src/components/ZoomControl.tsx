import { useEffect, useRef, useState } from "react";
import { ZoomIn, ZoomOut } from "lucide-react";

// A round zoom button that expands into a vertical slider on tap and collapses
// back after a couple of seconds of no interaction (UniFi-style). Higher value
// = zoomed in (top of the slider).
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
  const [expanded, setExpanded] = useState(false);
  const timer = useRef<number | undefined>(undefined);

  const arm = () => {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setExpanded(false), 2200);
  };
  useEffect(() => () => window.clearTimeout(timer.current), []);

  return (
    <div
      onPointerDown={arm}
      className={`flex flex-col items-center rounded-full bg-ink-700/95 backdrop-blur-sm overflow-hidden transition-all duration-300 ${
        expanded ? "h-[208px] w-11 py-2 justify-between" : "h-11 w-11 justify-center"
      } ${className}`}
    >
      <button
        className="h-9 w-9 grid place-items-center text-fg-muted shrink-0"
        aria-label="Zoom in"
        onClick={() => {
          if (!expanded) {
            setExpanded(true);
            arm();
          } else {
            onChange(Math.min(max, value + 1));
            arm();
          }
        }}
      >
        <ZoomIn className="h-4 w-4" />
      </button>
      {expanded && (
        <>
          <input
            type="range"
            min={0}
            max={max}
            step={1}
            value={value}
            onChange={(e) => {
              onChange(Number(e.target.value));
              arm();
            }}
            onPointerDown={arm}
            onPointerUp={arm}
            style={
              { width: 6, height: 120, WebkitAppearance: "slider-vertical", appearance: "slider-vertical" } as React.CSSProperties
            }
          />
          <button
            className="h-9 w-9 grid place-items-center text-fg-muted shrink-0"
            aria-label="Zoom out"
            onClick={() => {
              onChange(Math.max(0, value - 1));
              arm();
            }}
          >
            <ZoomOut className="h-4 w-4" />
          </button>
        </>
      )}
    </div>
  );
}
