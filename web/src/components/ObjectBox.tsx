import { useSyncExternalStore } from "react";
import { labelName } from "../lib/liveState";

// ObjectBox is how a recognised object is drawn everywhere (live tiles, the
// expanded view, playback): a thin white rounded box with a white tag above it
// reading "Person 87%". Coordinates are percentages of the picture.
export function ObjectBox({
  label,
  score,
  x,
  y,
  w,
  h,
  glide = 0,
}: {
  label: string;
  score: number;
  x: number;
  y: number;
  w: number;
  h: number;
  // Milliseconds over which the box glides to a new position (0 = jump).
  glide?: number;
}) {
  const nearTop = y < 0.07;
  return (
    <div
      className="absolute rounded-[5px] border-[1.5px] border-white/95 shadow-[0_0_0_1px_rgba(0,0,0,0.28),0_1px_8px_rgba(0,0,0,0.25)]"
      style={{
        left: `${x * 100}%`,
        top: `${y * 100}%`,
        width: `${w * 100}%`,
        height: `${h * 100}%`,
        transition: glide ? `left ${glide}ms linear, top ${glide}ms linear, width ${glide}ms linear, height ${glide}ms linear` : undefined,
      }}
    >
      <span
        className={`absolute left-[-1.5px] flex items-baseline gap-1 whitespace-nowrap rounded-[4px] bg-white/95 px-1.5 py-[2px] text-[11px] font-semibold leading-none tracking-[0.01em] text-ink-950 shadow-[0_1px_4px_rgba(0,0,0,0.25)] ${
          nearTop ? "top-[2px] left-[2px]" : "top-0 -translate-y-[calc(100%+3px)]"
        }`}
      >
        {labelName(label)}
        <span className="font-medium tabular-nums text-ink-950/55">{Math.round(score * 100)}%</span>
      </span>
    </div>
  );
}

// ---- the "Show object boxes" preference -------------------------------------

// One switch for every view: the button in Playback, the expanded live view
// and the camera wall all read and set the same preference (per browser).
const KEY = "aperture.objectBoxes";
const subs = new Set<() => void>();

function read(): boolean {
  try {
    return localStorage.getItem(KEY) !== "off";
  } catch {
    return true;
  }
}

export function setObjectBoxes(on: boolean) {
  try {
    localStorage.setItem(KEY, on ? "on" : "off");
  } catch {
    /* private mode: the switch just won't stick */
  }
  subs.forEach((fn) => fn());
}

if (typeof window !== "undefined") {
  window.addEventListener("storage", (e) => {
    if (e.key === KEY) subs.forEach((fn) => fn());
  });
}

export function useObjectBoxes(): [boolean, (on: boolean) => void] {
  const on = useSyncExternalStore(
    (fn) => {
      subs.add(fn);
      return () => subs.delete(fn);
    },
    read,
    () => true,
  );
  return [on, setObjectBoxes];
}
