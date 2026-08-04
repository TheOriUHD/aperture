import { useEffect, useRef, useState } from "react";
import type { MotionEvent } from "../lib/types";

// Draws object-detection bounding boxes over the player. Boxes are normalized
// [0..1] to the source frame, so we map them onto the video's *displayed* content
// rect (accounting for object-contain letterboxing). Detections are anchored at
// an event's start moment, so we show an event's boxes while the playhead sits
// within its window — a static box set per event, UniFi-style.
export function DetectionOverlay({
  events,
  atMs,
  videoRef,
  fit = "contain",
}: {
  events: MotionEvent[];
  atMs: number;
  videoRef: React.RefObject<HTMLVideoElement>;
  fit?: "cover" | "contain";
}) {
  const hostRef = useRef<HTMLDivElement>(null);
  const [rect, setRect] = useState({ left: 0, top: 0, width: 0, height: 0 });

  // Recompute the letterboxed content rect on size / intrinsic-dimension change.
  useEffect(() => {
    const host = hostRef.current;
    const vid = videoRef.current;
    if (!host) return;
    const measure = () => {
      const cw = host.clientWidth;
      const ch = host.clientHeight;
      const vw = vid?.videoWidth || 0;
      const vh = vid?.videoHeight || 0;
      if (!vw || !vh || !cw || !ch) {
        setRect({ left: 0, top: 0, width: cw, height: ch });
        return;
      }
      // cover fills the box (cropping the overflow); contain letterboxes.
      const scale = fit === "cover" ? Math.max(cw / vw, ch / vh) : Math.min(cw / vw, ch / vh);
      const w = vw * scale;
      const h = vh * scale;
      setRect({ left: (cw - w) / 2, top: (ch - h) / 2, width: w, height: h });
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(host);
    const v = videoRef.current;
    v?.addEventListener("loadedmetadata", measure);
    v?.addEventListener("resize", measure);
    return () => {
      ro.disconnect();
      v?.removeEventListener("loadedmetadata", measure);
      v?.removeEventListener("resize", measure);
    };
  }, [videoRef, fit]);

  // Events whose window covers the current time and that carry boxes. A little
  // slack so boxes don't flicker out exactly at the event edge.
  const active = events.filter(
    (e) => e.objects?.length && atMs >= e.startTs - 400 && atMs <= (e.endTs || e.startTs) + 400,
  );

  return (
    <div ref={hostRef} className="pointer-events-none absolute inset-0 z-[6] overflow-hidden">
      <div className="absolute" style={{ left: rect.left, top: rect.top, width: rect.width, height: rect.height }}>
        {active.flatMap((e) =>
          (e.objects ?? []).map((o, i) => (
            <div
              key={`${e.id}-${i}`}
              className="absolute rounded-[3px] border-2 border-brand/90 shadow-[0_0_0_1px_rgba(0,0,0,0.5)]"
              style={{
                left: `${o.x * 100}%`,
                top: `${o.y * 100}%`,
                width: `${o.w * 100}%`,
                height: `${o.h * 100}%`,
              }}
            >
              <span className="absolute left-0 top-0 -translate-y-full whitespace-nowrap rounded-t-[3px] bg-brand/90 px-1 text-[10px] font-medium leading-tight text-white">
                {o.label}
                {o.confidence ? ` ${Math.round(o.confidence * 100)}%` : ""}
              </span>
            </div>
          )),
        )}
      </div>
    </div>
  );
}
