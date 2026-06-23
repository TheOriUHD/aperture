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
 * Vertical time scrubber: the playhead is fixed near the top and the timeline
 * scrolls behind it. Drag or scroll to scrub. When at the live edge the playhead
 * reads LIVE and footage grows beneath it. onScrubTo always receives an absolute
 * target time so scrubbing is anchor-based and smooth.
 */
export function VerticalTimeline({
  selectedMs,
  windowMs,
  nowMs,
  segments,
  events,
  live,
  playheadFrac = 0.16,
  onScrubTo,
  onSeekAbs,
}: {
  selectedMs: number;
  windowMs: number;
  nowMs: number;
  segments: TR[];
  events: TR[];
  live: boolean;
  playheadFrac?: number;
  onScrubTo: (ms: number) => void;
  onSeekAbs: (ms: number) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const selRef = useRef(selectedMs);
  selRef.current = selectedMs;
  const drag = useRef<{ anchorY: number; anchorSel: number; moved: boolean } | null>(null);

  const topTime = selectedMs + playheadFrac * windowMs;
  const bottomTime = topTime - windowMs;
  const pct = (t: number) => ((topTime - t) / windowMs) * 100;
  const visible = (r: TR) => r.end > bottomTime && r.start < topTime;
  const railX = 58;

  // Non-passive wheel so scrubbing doesn't scroll the page; accumulate on a ref
  // for lag-free response to fast scrolls.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const next = selRef.current - (e.deltaY / el.clientHeight) * windowMs * 0.6;
      selRef.current = next;
      onScrubTo(next);
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [windowMs, onScrubTo]);

  const interval = majorInterval(windowMs);
  const majors: number[] = [];
  for (let t = Math.ceil(bottomTime / interval) * interval; t <= topTime; t += interval) majors.push(t);
  const minors: number[] = [];
  if (windowMs <= 60 * MIN) {
    const minorStep = interval / 5;
    for (let t = Math.ceil(bottomTime / minorStep) * minorStep; t <= topTime; t += minorStep) {
      if (Math.abs(t % interval) > 1) minors.push(t);
    }
  }

  function onPointerDown(e: React.PointerEvent<HTMLDivElement>) {
    drag.current = { anchorY: e.clientY, anchorSel: selRef.current, moved: false };
    e.currentTarget.setPointerCapture(e.pointerId);
  }
  function onPointerMove(e: React.PointerEvent<HTMLDivElement>) {
    const d = drag.current;
    if (!d || !ref.current) return;
    const dy = e.clientY - d.anchorY;
    if (Math.abs(dy) > 2) d.moved = true;
    onScrubTo(d.anchorSel + (dy / ref.current.clientHeight) * windowMs);
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
      onSeekAbs(topTime - ((e.clientY - rect.top) / rect.height) * windowMs);
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
      {minors.map((t) => (
        <div key={`mi${t}`} className="absolute h-px w-2 bg-line" style={{ left: railX - 10, top: `${pct(t)}%` }} />
      ))}
      {majors.map((t) => (
        <div key={`ma${t}`} className="absolute left-0 flex items-center gap-2" style={{ top: `${pct(t)}%` }}>
          <span className="w-10 text-right text-[11px] font-mono text-fg-faint -translate-y-1/2">{fmtHM(t)}</span>
          <div className="h-px w-3 bg-line-strong -translate-y-1/2" />
        </div>
      ))}

      <div className="absolute top-0 bottom-0 w-[3px] rounded-full bg-ink-850" style={{ left: railX }} />

      {segments.filter(visible).map((s, i) => (
        <div
          key={`s${i}`}
          className="absolute w-[3px] rounded-full bg-brand-deep"
          style={{ left: railX, top: `${pct(s.end)}%`, height: `${Math.max(pct(s.start) - pct(s.end), 0.2)}%` }}
        />
      ))}
      {events.filter(visible).map((s, i) => (
        <div
          key={`m${i}`}
          className="absolute w-[3px] rounded-full bg-motion"
          style={{ left: railX + 6, top: `${pct(s.end)}%`, height: `${Math.max(pct(s.start) - pct(s.end), 0.4)}%` }}
        />
      ))}

      {!live && nowMs <= topTime && nowMs >= bottomTime && (
        <div className="absolute left-12 right-0 border-t border-dashed border-online/50" style={{ top: `${pct(nowMs)}%` }} />
      )}

      {/* fixed playhead + pill */}
      <div className="absolute left-0 right-0 z-10 flex items-center" style={{ top: `${playheadFrac * 100}%` }}>
        <div
          className={`rounded-md px-2 py-1 text-[12px] font-medium tabular-nums shadow ${
            live ? "bg-online text-[#06251a]" : "bg-brand text-white"
          }`}
          style={{ minWidth: 66, textAlign: "center" }}
        >
          {live ? "LIVE" : fmtHMS(selectedMs)}
        </div>
        <div className={`flex-1 h-0.5 ${live ? "bg-online" : "bg-brand"}`} />
      </div>
    </div>
  );
}
