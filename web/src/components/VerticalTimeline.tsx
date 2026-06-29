import { useEffect, useRef, useState } from "react";
import { fmtTime, fmtTimeSec } from "../lib/format";
import { shadeColor, type ShadeBand } from "../lib/shading";

export interface TR {
  start: number;
  end: number;
}

export interface TLEvent {
  id: string;
  start: number;
  end: number;
  thumbUrl?: string;
}

const MIN = 60_000;
const NICE = [1, 2, 5, 10, 15, 30, 60, 120, 180, 360, 720].map((m) => m * MIN);

// Smallest "nice" interval that keeps labels at least minPx apart for the given
// pixel height, so labels are evenly spaced at every zoom.
function pickInterval(windowMs: number, heightPx: number, minPx: number): number {
  if (heightPx <= 0) return 5 * MIN;
  for (const n of NICE) {
    if ((n / windowMs) * heightPx >= minPx) return n;
  }
  return NICE[NICE.length - 1];
}

const fmtHM = fmtTime;
const fmtHMS = fmtTimeSec;

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
  shadeBands,
  ribbon,
  playheadFrac = 0.16,
  onScrubTo,
  onSeekAbs,
  onEventClick,
}: {
  selectedMs: number;
  windowMs: number;
  nowMs: number;
  segments: TR[];
  events: TLEvent[];
  live: boolean;
  shadeBands?: ShadeBand[];
  ribbon?: { ts: number; tempC?: number; precipMm?: number }[];
  playheadFrac?: number;
  onScrubTo: (ms: number) => void;
  onSeekAbs: (ms: number) => void;
  onEventClick: (startMs: number) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const selRef = useRef(selectedMs);
  selRef.current = selectedMs;
  const drag = useRef<{ anchorY: number; anchorSel: number; moved: boolean } | null>(null);
  const [height, setHeight] = useState(0);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setHeight(el.clientHeight));
    ro.observe(el);
    setHeight(el.clientHeight);
    return () => ro.disconnect();
  }, []);

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

  const interval = pickInterval(windowMs, height, 46);
  const majors: number[] = [];
  for (let t = Math.ceil(bottomTime / interval) * interval; t <= topTime; t += interval) majors.push(t);
  const minorStep = interval / 4;
  const minors: number[] = [];
  if ((minorStep / windowMs) * height >= 7) {
    for (let t = Math.ceil(bottomTime / minorStep) * minorStep; t <= topTime; t += minorStep) {
      if (Math.abs(t % interval) > 1) minors.push(t);
    }
  }

  // Visible events, and which ones get a thumbnail (avoid overlapping squares).
  const visEvents = events.filter((e) => e.end > bottomTime && e.start < topTime);
  const thumbIds = new Set<string>();
  {
    let last = -100;
    for (const e of visEvents) {
      const c = pct((e.start + e.end) / 2);
      if (Math.abs(c - last) >= 4.5) {
        thumbIds.add(e.id);
        last = c;
      }
    }
  }

  // Environmental ribbon: a temperature trend (warmer = further right) with
  // precipitation ticks, drawn down the right margin where nothing else lives.
  const ribbonEl = (() => {
    const pts = (ribbon ?? []).filter((r) => r.tempC != null);
    if (pts.length < 2) return null;
    const temps = pts.map((r) => r.tempC as number);
    let lo = Math.min(...temps);
    let hi = Math.max(...temps);
    if (hi - lo < 1) hi = lo + 1;
    const x0 = 74;
    const x1 = 96;
    const xOf = (t: number) => x0 + ((t - lo) / (hi - lo)) * (x1 - x0);
    const line = pts.map((r) => `${xOf(r.tempC as number).toFixed(2)},${pct(r.ts * 1000).toFixed(2)}`).join(" ");
    const rain = (ribbon ?? []).filter((r) => (r.precipMm ?? 0) > 0);
    return (
      <svg
        className="absolute inset-0 h-full w-full pointer-events-none"
        viewBox="0 0 100 100"
        preserveAspectRatio="none"
      >
        {rain.map((r, i) => (
          <rect key={`p${i}`} x={66} y={pct(r.ts * 1000) - 0.4} width={4} height={0.8} fill="rgba(80,150,240,0.65)" />
        ))}
        <polyline
          points={line}
          fill="none"
          stroke="rgba(245,180,90,0.7)"
          strokeWidth={1.2}
          vectorEffect="non-scaling-stroke"
          strokeLinejoin="round"
        />
      </svg>
    );
  })();

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
      {/* Day/night shading from the astro layer, behind everything. */}
      {shadeBands?.map((b, i) => {
        const top = pct(b.end);
        const h = pct(b.start) - pct(b.end);
        if (h <= 0 || top >= 100 || top + h <= 0) return null;
        return (
          <div
            key={`sh${i}`}
            className="absolute left-0 right-0 pointer-events-none"
            style={{ top: `${top}%`, height: `${h}%`, backgroundColor: shadeColor(b.kind) }}
          />
        );
      })}

      {minors.map((t) => (
        <div key={`mi${t}`} className="absolute h-px w-2 bg-line" style={{ left: railX - 10, top: `${pct(t)}%` }} />
      ))}
      {majors.map((t) => (
        <div key={`ma${t}`} className="absolute left-0 flex items-center gap-2" style={{ top: `${pct(t)}%` }}>
          <span className="w-10 text-right text-[11px] font-mono text-fg-faint -translate-y-1/2 whitespace-nowrap">
            {fmtHM(t)}
          </span>
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
      {/* motion: lighter-blue highlight over the rail + dot + thumbnail square */}
      {visEvents.map((e) => {
        const cy = pct((e.start + e.end) / 2);
        return (
          <div key={e.id}>
            <div
              className="absolute w-[3px] rounded-full bg-motion"
              style={{ left: railX, top: `${pct(e.end)}%`, height: `${Math.max(pct(e.start) - pct(e.end), 0.5)}%` }}
            />
            <div
              className="absolute h-2 w-2 rounded-full bg-motion border border-ink-900"
              style={{ left: railX + 1.5, top: `${cy}%`, transform: "translate(-50%,-50%)" }}
            />
            {thumbIds.has(e.id) && (
              <button
                onPointerDown={(ev) => ev.stopPropagation()}
                onClick={(ev) => {
                  ev.stopPropagation();
                  onEventClick(e.start);
                }}
                className="absolute rounded-sm overflow-hidden border border-motion/70 hover:border-motion bg-ink-800 shadow"
                style={{ left: railX + 12, top: `${cy}%`, transform: "translateY(-50%)", width: 38, height: 26 }}
                aria-label="Jump to motion event"
              >
                {e.thumbUrl ? (
                  <img src={e.thumbUrl} alt="" className="h-full w-full object-cover" />
                ) : (
                  <div className="h-full w-full bg-ink-700" />
                )}
              </button>
            )}
          </div>
        );
      })}

      {ribbonEl}

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
