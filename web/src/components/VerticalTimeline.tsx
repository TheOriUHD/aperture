import { useEffect, useRef } from "react";

export interface TR {
  start: number;
  end: number;
}

const MIN = 60_000;

function tickInterval(windowMs: number): number {
  if (windowMs <= 10 * MIN) return MIN;
  if (windowMs <= 30 * MIN) return 5 * MIN;
  if (windowMs <= 60 * MIN) return 10 * MIN;
  if (windowMs <= 180 * MIN) return 30 * MIN;
  if (windowMs <= 360 * MIN) return 60 * MIN;
  return 120 * MIN;
}

function fmtHM(ms: number) {
  return new Date(ms).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

/**
 * Vertical, zoomable time scrubber (UniFi Protect style): newest at top, older
 * downward. Click to seek, wheel to pan through time.
 */
export function VerticalTimeline({
  topMs,
  windowMs,
  nowMs,
  segments,
  events,
  playheadMs,
  onSeek,
  onPan,
}: {
  topMs: number;
  windowMs: number;
  nowMs: number;
  segments: TR[];
  events: TR[];
  playheadMs?: number;
  onSeek: (ms: number) => void;
  onPan: (deltaMs: number) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const bottomMs = topMs - windowMs;
  const pct = (t: number) => ((topMs - t) / windowMs) * 100;

  // Non-passive wheel listener so we can pan without scrolling the page.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      onPan((e.deltaY / el.clientHeight) * windowMs);
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [windowMs, onPan]);

  const interval = tickInterval(windowMs);
  const ticks: number[] = [];
  for (let t = Math.ceil(bottomMs / interval) * interval; t <= topMs; t += interval) {
    ticks.push(t);
  }

  const visible = (r: TR) => r.end > bottomMs && r.start < topMs;

  function handleClick(e: React.MouseEvent<HTMLDivElement>) {
    const rect = e.currentTarget.getBoundingClientRect();
    const frac = Math.min(1, Math.max(0, (e.clientY - rect.top) / rect.height));
    onSeek(topMs - frac * windowMs);
  }

  return (
    <div
      ref={ref}
      onClick={handleClick}
      className="relative h-full w-full cursor-pointer select-none overflow-hidden"
    >
      {/* tick labels + gridlines */}
      {ticks.map((t) => (
        <div key={t} className="absolute left-0 right-0 flex items-center" style={{ top: `${pct(t)}%` }}>
          <span className="w-12 text-[11px] font-mono text-fg-faint -translate-y-1/2">{fmtHM(t)}</span>
          <div className="flex-1 h-px bg-line/60" />
        </div>
      ))}

      {/* central rail */}
      <div className="absolute top-0 bottom-0 left-14 w-1.5 rounded bg-ink-850" />

      {/* recorded spans */}
      {segments.filter(visible).map((s, i) => (
        <div
          key={`s${i}`}
          className="absolute left-14 w-1.5 rounded bg-brand-deep"
          style={{ top: `${pct(s.end)}%`, height: `${Math.max(pct(s.start) - pct(s.end), 0.3)}%` }}
        />
      ))}
      {/* motion spans (offset to a second lane) */}
      {events.filter(visible).map((s, i) => (
        <div
          key={`m${i}`}
          className="absolute w-1.5 rounded bg-motion"
          style={{
            left: 70,
            top: `${pct(s.end)}%`,
            height: `${Math.max(pct(s.start) - pct(s.end), 0.6)}%`,
          }}
        />
      ))}

      {/* now marker */}
      {nowMs <= topMs && nowMs >= bottomMs && (
        <div className="absolute left-12 right-0 flex items-center" style={{ top: `${pct(nowMs)}%` }}>
          <div className="flex-1 h-px bg-online/60" />
        </div>
      )}

      {/* playhead */}
      {playheadMs !== undefined && playheadMs <= topMs && playheadMs >= bottomMs && (
        <div className="absolute left-0 right-0 z-10 flex items-center" style={{ top: `${pct(playheadMs)}%` }}>
          <div className="w-full h-0.5 bg-white" />
          <div className="absolute left-14 h-3 w-3 -translate-x-1/2 -translate-y-1/2 rounded-full bg-white border-2 border-ink-900" />
        </div>
      )}
    </div>
  );
}
