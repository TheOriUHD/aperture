import { useMemo } from "react";

export interface TimeRange {
  start: number; // unix ms
  end: number; // unix ms
}

const DAY_MS = 24 * 60 * 60 * 1000;
const HOURS = [0, 4, 8, 12, 16, 20, 24];

/**
 * A 24-hour timeline scrubber. Continuous recordings render as blue spans,
 * motion events as amber. Positions are a percentage of the day so it stays
 * resolution-independent.
 */
export function Timeline({
  dayStart,
  segments,
  events,
  playheadMs,
}: {
  dayStart: number;
  segments: TimeRange[];
  events: TimeRange[];
  playheadMs?: number;
}) {
  const spans = useMemo(() => toSpans(segments, dayStart), [segments, dayStart]);
  const motion = useMemo(() => toSpans(events, dayStart), [events, dayStart]);
  const playPct = playheadMs !== undefined ? clampPct(((playheadMs - dayStart) / DAY_MS) * 100) : undefined;
  const empty = spans.length === 0 && motion.length === 0;

  return (
    <div>
      <div className="relative h-10 rounded-md bg-ink-850 border border-line overflow-hidden">
        {/* hour gridlines */}
        {HOURS.slice(1, -1).map((h) => (
          <div
            key={h}
            className="absolute top-0 bottom-0 w-px bg-line/70"
            style={{ left: `${(h / 24) * 100}%` }}
          />
        ))}
        {spans.map((s, i) => (
          <div
            key={`s${i}`}
            className="absolute top-0 bottom-0 bg-brand-deep/85"
            style={{ left: `${s.left}%`, width: `${s.width}%` }}
          />
        ))}
        {motion.map((s, i) => (
          <div
            key={`m${i}`}
            className="absolute top-0 bottom-0 bg-motion"
            style={{ left: `${s.left}%`, width: `${Math.max(s.width, 0.4)}%` }}
          />
        ))}
        {playPct !== undefined && (
          <div className="absolute -top-1 -bottom-1 z-10" style={{ left: `${playPct}%` }}>
            <div className="w-0.5 h-full bg-white" />
            <div className="absolute -top-1 -left-[5px] h-3 w-3 rounded-full bg-white border-2 border-ink-900" />
          </div>
        )}
        {empty && (
          <div className="absolute inset-0 grid place-items-center text-xs text-fg-faint">
            No footage for this day
          </div>
        )}
      </div>
      <div className="flex justify-between mt-1.5 text-[11px] font-mono text-fg-faint">
        {HOURS.map((h) => (
          <span key={h}>{String(h).padStart(2, "0")}:00</span>
        ))}
      </div>
    </div>
  );
}

function toSpans(ranges: TimeRange[], dayStart: number) {
  const out: { left: number; width: number }[] = [];
  for (const r of ranges) {
    const left = clampPct(((r.start - dayStart) / DAY_MS) * 100);
    const right = clampPct(((r.end - dayStart) / DAY_MS) * 100);
    if (right <= 0 || left >= 100) continue;
    out.push({ left, width: Math.max(right - left, 0.1) });
  }
  return out;
}

function clampPct(v: number) {
  return Math.min(100, Math.max(0, v));
}
