import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import {
  Bike,
  Bird,
  Bus,
  Car,
  Cat,
  Dog,
  Package,
  PawPrint,
  PersonStanding,
  ScanEye,
  Truck,
  type LucideIcon,
} from "lucide-react";
import { labelName, PARKED_TTL, useLiveCamera } from "../lib/liveState";
import type { ContentRect } from "./useContentRect";

// objectIcon is the little picture a box carries for its label.
export function objectIcon(label: string): LucideIcon {
  switch (label) {
    case "person":
      return PersonStanding;
    case "car":
      return Car;
    case "truck":
      return Truck;
    case "bus":
      return Bus;
    case "bicycle":
    case "motorcycle":
      return Bike;
    case "dog":
      return Dog;
    case "cat":
      return Cat;
    case "bird":
      return Bird;
    case "horse":
    case "sheep":
    case "cow":
    case "bear":
      return PawPrint;
    case "package":
      return Package;
  }
  return ScanEye;
}

// ObjectBox is how a recognised object is drawn everywhere (live tiles, the
// expanded view, playback): a thin white rounded box with a small white tag
// above it showing what it is as an icon. Hovered, the tag also reads
// "Person 87%". Coordinates are fractions of the picture.
export function ObjectBox({
  label,
  score,
  x,
  y,
  w,
  h,
  glide = 0,
  expanded = false,
}: {
  label: string;
  score: number;
  x: number;
  y: number;
  w: number;
  h: number;
  // Milliseconds over which the box glides to a new position (0 = jump).
  glide?: number;
  // Show the name and score beside the icon (the pointer is on the box).
  expanded?: boolean;
}) {
  const nearTop = y < 0.07;
  const Icon = objectIcon(label);
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
        className={`absolute left-[-1.5px] flex h-[18px] items-center gap-1 whitespace-nowrap rounded-[4px] bg-white/95 text-[11px] font-semibold leading-none tracking-[0.01em] text-ink-950 shadow-[0_1px_4px_rgba(0,0,0,0.25)] ${
          expanded ? "pl-1 pr-1.5" : "w-[18px] justify-center"
        } ${nearTop ? "top-[2px] left-[2px]" : "top-0 -translate-y-[calc(100%+3px)]"}`}
        title={expanded ? undefined : `${labelName(label)} ${Math.round(score * 100)}%`}
      >
        <Icon className="h-3 w-3 shrink-0" strokeWidth={2.4} />
        {expanded && (
          <>
            {labelName(label)}
            <span className="font-medium tabular-nums text-ink-950/55">{Math.round(score * 100)}%</span>
          </>
        )}
      </span>
    </div>
  );
}

// ---- which box the pointer is on --------------------------------------------

type Placed = { key: string; x: number; y: number; w: number; h: number };

// useHoveredBox reports the box under the mouse pointer (the smallest, where
// boxes overlap). The overlay lets every click through to the picture, and
// other layers (controls, zoom) may lie above it, so it follows the pointer
// over the whole page and checks whether it is over this picture.
export function useHoveredBox(
  hostRef: React.RefObject<HTMLElement>,
  rect: ContentRect,
  boxes: Placed[],
): string | null {
  const pointer = useRef<{ x: number; y: number } | null>(null);
  const latest = useRef({ rect, boxes });
  latest.current = { rect, boxes };
  const [hovered, setHovered] = useState<string | null>(null);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    let raf = 0;
    const leave = () => {
      pointer.current = null;
      setHovered(null);
    };
    const move = (e: PointerEvent) => {
      if (e.pointerType !== "mouse") return; // no hover on a touch screen
      const r = host.getBoundingClientRect();
      const inside = e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom;
      if (!inside) {
        if (pointer.current) leave();
        return;
      }
      // Playback zoom scales the overlay: back to its own pixels.
      const sx = host.clientWidth ? r.width / host.clientWidth : 1;
      const sy = host.clientHeight ? r.height / host.clientHeight : 1;
      pointer.current = { x: (e.clientX - r.left) / sx, y: (e.clientY - r.top) / sy };
      if (!raf) {
        raf = requestAnimationFrame(() => {
          raf = 0;
          setHovered(pick(pointer.current, latest.current.rect, latest.current.boxes));
        });
      }
    };
    window.addEventListener("pointermove", move, { passive: true });
    document.documentElement.addEventListener("pointerleave", leave);
    return () => {
      window.removeEventListener("pointermove", move);
      document.documentElement.removeEventListener("pointerleave", leave);
      cancelAnimationFrame(raf);
    };
  }, [hostRef]);

  // Boxes move under a still pointer too.
  const now = pick(pointer.current, rect, boxes);
  return pointer.current ? now : hovered;
}

function pick(p: { x: number; y: number } | null, rect: ContentRect, boxes: Placed[]): string | null {
  if (!p || rect.width <= 0 || rect.height <= 0) return null;
  const px = (p.x - rect.left) / rect.width;
  const py = (p.y - rect.top) / rect.height;
  let best: string | null = null;
  let bestArea = Infinity;
  for (const b of boxes) {
    // The tag above the box counts as part of it (about 20 px).
    const tag = 22 / rect.height;
    if (px >= b.x && px <= b.x + b.w && py >= b.y - tag && py <= b.y + b.h && b.w * b.h < bestArea) {
      best = b.key;
      bestArea = b.w * b.h;
    }
  }
  return best;
}

// ---- parked vehicles --------------------------------------------------------

// ParkedBadge is the blue "P" a camera shows while a vehicle stands parked in
// view (recognised, but not moving: so it gets no box). It follows the "Show
// object boxes" switch like the boxes do.
export function ParkedBadge({ cameraId, className = "" }: { cameraId: string; className?: string }) {
  const live = useLiveCamera(cameraId);
  const [boxesOn] = useObjectBoxes();
  const [, setTick] = useState(0);
  const age = Date.now() - live.parkedAt;
  const show = boxesOn && live.parked > 0 && age < PARKED_TTL;
  useEffect(() => {
    if (!show) return;
    const t = window.setTimeout(() => setTick((n) => n + 1), PARKED_TTL - age + 20);
    return () => window.clearTimeout(t);
  }, [show, age, live.parkedAt]);
  if (!show) return null;
  const n = live.parked;
  return (
    <span
      title={n === 1 ? "A parked vehicle" : `${n} parked vehicles`}
      aria-label={n === 1 ? "A parked vehicle" : `${n} parked vehicles`}
      className={`inline-flex h-[18px] min-w-[18px] shrink-0 items-center justify-center gap-[1px] rounded-[4px] bg-brand px-[3px] text-[11.5px] font-bold leading-none text-white shadow-[0_0_0_1px_rgba(0,0,0,0.3),0_1px_4px_rgba(0,0,0,0.3)] ${className}`}
    >
      P{n > 1 && <span className="text-[9px] font-semibold tabular-nums">{n}</span>}
    </span>
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
