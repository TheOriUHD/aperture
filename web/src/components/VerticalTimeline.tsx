import { useEffect, useRef } from "react";

export interface TR {
  start: number;
  end: number;
}

const MIN = 60_000;

function majorInterval(windowMs: number): number {
  if (windowMs <= 10 * MIN) return MIN;
  if (windowMs <= 30 * MIN) return 2 * MIN;
  if (windowMs <= 60 * MIN) return 5 * MIN;
  if (windowMs <= 180 * MIN) return 15 * MIN;
  if (windowMs <= 360 * MIN) return 30 * MIN;
  return 60 * MIN;
}

const fmtHM = (ms: number) =>
  new Date(ms).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
const fmtHMS = (ms: number) =>
  new Date(ms).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" });

/**
 * Vertical time scrubber, UniFi-style: the playhead is fixed near the top and
 * the timeline scrolls behind it. Drag or scroll to scrub through time. Newest
 * is up, older is down.
 */
export function VerticalTimeline({
  selectedMs,
  windowMs,
  nowMs,
  segments,
  events,
  following,
  playheadFrac = 0.16,
  onScrub,
  onSeekTo,
}: {
  selectedMs: number;
  windowMs: number;
  nowMs: number;
  segments: TR[];
  events: TR[];
  following: boolean;
  playheadFrac?: number;
  onScrub: (deltaMs: number) => void;
  onSeekTo: (ms: number) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const drag = useRef<{ moved: boolean; pid: number } | null>(null);

  const topTime = selectedMs + playheadFrac * windowMs;
  const bottomTime = topTime - windowMs;
  const pct = (t: number) => ((topTime - t) / windowMs) * 100;
  const visible = (r: TR) => r.end > bottomTime && r.start < topTime;
  const railX = 58;

  // Non-passive wheel so we can scrub without scrolling the page.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      onScrub(-(e.deltaY / el.clientHeight) * windowMs * 0.5);
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [windowMs, onScrub]);

  const interval = majorInterval(windowMs);
  const majors: number[] = [];
  for (let t = Math.ceil(bottomTime / interval) * interval; t <= topTime; t += interval) majors.push(t);
  const minorStep = interval / 5;
  const minors: number[] = [];
  if (windowMs <= 60 * MIN) {
    for (let t = Math.ceil(bottomTime / minorStep) * minorStep; t <= topTime; t += minorStep) {
      if (Math.abs(t % interval) > 1) minors.push(t);
    }
  }

  function onPointerDown(e: React.PointerEvent<HTMLDivElement>) {
    drag.current = { moved: false, pid: e.pointerId };
    e.currentTarget.setPointerCapture(e.pointerId);
  }
  function onPointerMove(e: React.PointerEvent<HTMLDivElement>) {
    if (!drag.current || !ref.current) return;
    if (e.movementY !== 0) {
      if (Math.abs(e.movementY) > 1) drag.current.moved = true;
      onScrub((e.movementY * windowMs) / ref.current.clientHeight);
    }
  }
  function onPointerUp(e: React.PointerEvent<HTMLDivElement>) {
    const d = drag.current;
    drag.current = null;
    try {
      e.currentTarget.releasePointerCapture(e.pointerId);
    } catch {
      /* ignore */
    }
    if (d && !d.moved && ref.current) {
      const rect = ref.current.getBoundingClientRect();
      const frac = (e.clientY - rect.top) / rect.height;
      onSeekTo(topTime - frac * windowMs);
    }
  }

  return (
    <div
      ref={ref}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      className="relative h-full w-full select-none overflow-hidden cursor-grab active:cursor-grabbing touch-none"
    >
      {/* minor ticks */}
      {minors.map((t) => (
        <div
          key={`mi${t}`}
          className="absolute h-px w-2 bg-line"
          style={{ left: railX - 10, top: `${pct(t)}%` }}
        />
      ))}

      {/* major ticks + labels */}
      {majors.map((t) => (
        <div key={`ma${t}`} className="absolute left-0 flex items-center gap-2" style={{ top: `${pct(t)}%` }}>
          <span className="w-10 text-right text-[11px] font-mono text-fg-faint -translate-y-1/2">
            {fmtHM(t)}
          </span>
          <div className="h-px w-3 bg-line-strong -translate-y-1/2" />
        </div>
      ))}

      {/* rail */}
      <div className="absolute top-0 bottom-0 w-[3px] rounded-full bg-ink-850" style={{ left: railX }} />

      {/* recorded spans (solid, merged) */}
      {segments.filter(visible).map((s, i) => (
        <div
          key={`s${i}`}
          className="absolute w-[3px] rounded-full bg-brand-deep"
          style={{ left: railX, top: `${pct(s.end)}%`, height: `${Math.max(pct(s.start) - pct(s.end), 0.2)}%` }}
        />
      ))}
      {/* motion spans */}
      {events.filter(visible).map((s, i) => (
        <div
          key={`m${i}`}
          className="absolute w-[3px] rounded-full bg-motion"
          style={{ left: railX + 6, top: `${pct(s.end)}%`, height: `${Math.max(pct(s.start) - pct(s.end), 0.4)}%` }}
        />
      ))}

      {/* now marker (only when scrubbed into the past) */}
      {!following && nowMs <= topTime && nowMs >= bottomTime && (
        <div
          className="absolute left-12 right-0 border-t border-dashed border-online/50"
          style={{ top: `${pct(nowMs)}%` }}
        />
      )}

      {/* fixed playhead + time pill */}
      <div className="absolute left-0 right-0 z-10 flex items-center" style={{ top: `${playheadFrac * 100}%` }}>
        <div
          className="rounded-md bg-brand px-2 py-1 text-[12px] font-medium text-white tabular-nums shadow"
          style={{ minWidth: 78, textAlign: "center" }}
        >
          {fmtHMS(selectedMs)}
        </div>
        <div className="flex-1 h-0.5 bg-brand" />
      </div>
    </div>
  );
}
