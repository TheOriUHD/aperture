import { useEffect, useLayoutEffect, useRef, useState } from "react";
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
  following = false,
  shadeBands,
  ribbon,
  gaps,
  playheadFrac = 0.2,
  onScrubTo,
  onScrubEnd,
  onSeekAbs,
  onEventClick,
}: {
  selectedMs: number;
  windowMs: number;
  nowMs: number;
  segments: TR[];
  events: TLEvent[];
  live: boolean;
  // True only while the player is genuinely live-following (auto-advancing at
  // real time). Gates the smooth-glide transform so it never drifts when paused
  // or scrubbed near the edge — distinct from `live` (playhead-at-edge display).
  following?: boolean;
  shadeBands?: ShadeBand[];
  ribbon?: { ts: number; tempC?: number; precipMm?: number }[];
  gaps?: TR[];
  playheadFrac?: number;
  onScrubTo: (ms: number) => void;
  onScrubEnd?: () => void;
  onSeekAbs: (ms: number) => void;
  onEventClick: (startMs: number) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const selRef = useRef(selectedMs);
  selRef.current = selectedMs;
  const nowRef = useRef(nowMs);
  nowRef.current = nowMs;
  const liveRef = useRef(live);
  liveRef.current = live;
  const drag = useRef<{ anchorY: number; anchorSel: number; moved: boolean } | null>(null);
  const [height, setHeight] = useState(0);
  const scrollRef = useRef<HTMLDivElement>(null);
  // Smooth-scroll baseline: the rendered position (`selectedMs`) and the moment
  // it was committed. Between the 1 Hz live ticks we translate the strip by the
  // real time elapsed since this baseline, so the live edge glides instead of
  // stepping once a second — without re-rendering React every frame.
  const liveBaseRef = useRef({ ms: selectedMs, at: 0 });

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setHeight(el.clientHeight));
    ro.observe(el);
    setHeight(el.clientHeight);
    return () => ro.disconnect();
  }, []);

  // The playhead stays LOCKED at playheadFrac; the strip scrolls behind it.
  const topTime = selectedMs + playheadFrac * windowMs;
  const bottomTime = topTime - windowMs;
  const pct = (t: number) => ((topTime - t) / windowMs) * 100;
  // But never draw tick labels / rail past "now" — no future zone above LIVE.
  const topTick = Math.min(topTime, nowMs);
  // Treat an over-scroll into the future zone as "live" for the pill, so pulling
  // toward the edge reads LIVE rather than flashing a future timestamp.
  const atLive = live || selectedMs >= nowMs;
  const visible = (r: TR) => r.end > bottomTime && r.start < topTime;
  const railX = 58;

  // Non-passive wheel so scrubbing doesn't scroll the page; accumulate on a ref
  // for lag-free response to fast scrolls.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const raw = selRef.current - (e.deltaY / el.clientHeight) * windowMs * 0.6;
      // Already live and still pushing forward? ignore (no flip-flop).
      if (liveRef.current && raw >= nowRef.current) return;
      // No wall above live: allow a generous over-scroll and let the parent's
      // progressive rubber-band (applyScrub) compress it — the settle then
      // springs it into live. The cap only stops the accumulator running away.
      const capped = Math.min(raw, nowRef.current + windowMs * 0.6);
      selRef.current = capped;
      onScrubTo(capped);
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [windowMs, onScrubTo]);

  // Reset the smooth-scroll baseline the instant the rendered position changes
  // (before paint), so the transform below never double-counts a tick — the 1s
  // content jump and the transform reset cancel exactly, leaving no seam.
  useLayoutEffect(() => {
    liveBaseRef.current = { ms: selectedMs, at: performance.now() };
    // Zero the transform in the SAME commit as the content jump, so a stale
    // per-frame offset can never paint on top of the new positions (no seam).
    if (following && scrollRef.current) scrollRef.current.style.transform = "translateY(0px)";
  }, [selectedMs, following]);

  // While live, glide the strip between ticks with a GPU transform (no React
  // re-render, so no jank). The offset is tiny — one tick's worth (~a few px) —
  // it just fills the gap between the 1 Hz clock updates. Cleared when not live
  // so scrubbed/recorded positions render exactly where computed.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (!following) {
      el.style.transform = "";
      return;
    }
    let raf = 0;
    const tick = () => {
      const elapsed = Math.min(performance.now() - liveBaseRef.current.at, 2000);
      el.style.transform = `translateY(${(elapsed / windowMs) * (el.clientHeight || height)}px)`;
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(raf);
      if (scrollRef.current) scrollRef.current.style.transform = "";
    };
  }, [following, windowMs, height]);

  const interval = pickInterval(windowMs, height, 46);
  const majors: number[] = [];
  for (let t = Math.ceil(bottomTime / interval) * interval; t <= topTick; t += interval) majors.push(t);
  const minorStep = interval / 4;
  const minors: number[] = [];
  if ((minorStep / windowMs) * height >= 7) {
    for (let t = Math.ceil(bottomTime / minorStep) * minorStep; t <= topTick; t += minorStep) {
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
    if (d && ref.current) {
      const dy = e.clientY - d.anchorY;
      if (Math.abs(dy) > 2) d.moved = true;
      onScrubTo(d.anchorSel + (dy / ref.current.clientHeight) * windowMs);
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
      onSeekAbs(topTime - ((e.clientY - rect.top) / rect.height) * windowMs);
    } else if (d && d.moved) {
      // Gesture ended: settle now (bounce back to live if we're near/over the
      // edge) instead of waiting out the scrub debounce.
      onScrubEnd?.();
    }
  }
  function onPointerCancel() {
    const d = drag.current;
    drag.current = null;
    if (d?.moved) onScrubEnd?.();
  }

  return (
    <div
      ref={ref}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerCancel}
      className="relative h-full w-full select-none overflow-hidden cursor-grab active:cursor-grabbing touch-none"
    >
      {/* Scrolling strip layer — GPU-translated for smooth live glide. Everything
          time-positioned lives in here; the fixed playhead stays outside it. */}
      <div ref={scrollRef} className="absolute inset-0">
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
      {/* "No recording" gaps (continuous cameras only) — hatched red over the rail. */}
      {gaps?.filter(visible).map((g, i) => (
        <div
          key={`g${i}`}
          className="absolute w-[3px] rounded-full"
          style={{
            left: railX,
            top: `${pct(g.end)}%`,
            height: `${Math.max(pct(g.start) - pct(g.end), 0.2)}%`,
            background: "repeating-linear-gradient(180deg, rgba(224,80,80,0.65) 0 3px, transparent 3px 6px)",
          }}
          title="No recording"
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

      {/* Live-edge separator: divides the recording zone (below) from the empty
          future / live zone (above). Only when scrubbed away — at rest on live
          the playhead's own LIVE bar IS this divider, so we don't double it up. */}
      {!live && nowMs <= topTime && nowMs >= bottomTime && (
        <div
          className="absolute z-[5] h-px bg-online/70"
          style={{ left: railX - 5, right: 0, top: `${pct(nowMs)}%` }}
        />
      )}
      </div>
      {/* end scrolling strip layer */}

      {/* fixed playhead + pill (locked at playheadFrac; strip scrolls behind).
          Centered on its time line so the bar sits exactly on now/selected —
          recordings butt right up to it instead of spilling above. */}
      <div
        className="absolute left-0 right-0 z-10 flex items-center -translate-y-1/2"
        style={{ top: `${playheadFrac * 100}%` }}
      >
        <div
          className={`rounded-md px-2 py-1 text-[12px] font-medium tabular-nums shadow ${
            atLive ? "bg-online text-[#06251a]" : "bg-brand text-white"
          }`}
          style={{ minWidth: 66, textAlign: "center" }}
        >
          {atLive ? "LIVE" : fmtHMS(selectedMs)}
        </div>
        <div className={`flex-1 h-0.5 ${atLive ? "bg-online" : "bg-brand"}`} />
      </div>
    </div>
  );
}
