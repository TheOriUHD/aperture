import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import type { MotionEvent } from "../lib/types";
import { ObjectBox } from "./ObjectBox";
import { useContentRect } from "./useContentRect";

// One recognised object's position at one moment (GET /cameras/{id}/tracks).
export type TrackPoint = {
  eventId: string;
  obj: number;
  label: string;
  confidence: number;
  x: number;
  y: number;
  w: number;
  h: number;
  ts: number;
};

const BUCKET = 10 * 60_000; // tracks are fetched ten minutes at a time
const HOLD = 700; // a box stays this long after its last known position
const MAX_GAP = 2500; // longer gaps between two positions aren't bridged

// DetectionOverlay draws recognised objects over recorded video. Objects the
// live recogniser followed have a path, and their box glides along it as the
// video plays, interpolated between the stored positions every frame. Older
// events (or an external detector) only have a best sighting; those show as a
// still box while the playhead is inside the event.
export function DetectionOverlay({
  cameraId,
  events,
  atMs,
  videoRef,
  fit = "contain",
}: {
  cameraId: string;
  events: MotionEvent[];
  atMs: number;
  videoRef: React.RefObject<HTMLVideoElement>;
  fit?: "cover" | "contain";
}) {
  const hostRef = useRef<HTMLDivElement>(null);
  const rect = useContentRect(hostRef, videoRef, fit);

  // Paths around the playhead: this ten-minute bucket and its neighbours'
  // edges, refreshed while the window still includes "now".
  const bucket = Math.floor(atMs / BUCKET);
  const from = bucket * BUCKET - 60_000;
  const to = (bucket + 1) * BUCKET + 60_000;
  const { data: points } = useQuery({
    queryKey: ["tracks", cameraId, bucket],
    queryFn: () => api.get<TrackPoint[]>(`/api/v1/cameras/${cameraId}/tracks?from=${from}&to=${to}`),
    refetchInterval: to > Date.now() ? 5000 : false,
    staleTime: 60_000,
  });

  // Group into paths: one per object per event, in time order.
  const paths = useMemo(() => {
    const m = new Map<string, TrackPoint[]>();
    for (const p of points ?? []) {
      const k = `${p.eventId}:${p.obj}`;
      let arr = m.get(k);
      if (!arr) m.set(k, (arr = []));
      arr.push(p);
    }
    return [...m.entries()].map(([key, pts]) => ({ key, pts }));
  }, [points]);
  const tracked = useMemo(() => new Set((points ?? []).map((p) => p.eventId)), [points]);

  // Follow the playhead between its updates (a few a second) so the boxes
  // glide at the video's frame rate.
  const anchor = useRef({ ms: atMs, perf: performance.now() });
  useEffect(() => {
    anchor.current = { ms: atMs, perf: performance.now() };
  }, [atMs]);
  const [now, setNow] = useState(atMs);
  useEffect(() => {
    let raf = 0;
    const step = () => {
      const v = videoRef.current;
      const playing = v && !v.paused && !v.ended && v.readyState >= 2;
      const a = anchor.current;
      const t = playing ? a.ms + Math.min(1000, (performance.now() - a.perf) * (v!.playbackRate || 1)) : a.ms;
      setNow((prev) => (Math.abs(prev - t) > 8 ? t : prev));
      raf = requestAnimationFrame(step);
    };
    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
  }, [videoRef]);

  const boxes: { key: string; label: string; score: number; x: number; y: number; w: number; h: number }[] = [];
  for (const { key, pts } of paths) {
    const b = at(pts, now);
    if (b) boxes.push({ key, ...b });
  }
  // Events without a path: their best sighting, while the playhead is inside.
  for (const e of events) {
    if (tracked.has(e.id) || !e.objects?.length) continue;
    if (now < e.startTs - 400 || now > (e.endTs || e.startTs) + 400) continue;
    e.objects.forEach((o, i) =>
      boxes.push({ key: `${e.id}-s${i}`, label: o.label, score: o.confidence ?? 0, x: o.x, y: o.y, w: o.w, h: o.h }),
    );
  }

  return (
    <div ref={hostRef} className="pointer-events-none absolute inset-0 z-[6] overflow-hidden">
      <div className="absolute" style={rect}>
        {boxes.map(({ key, ...b }) => (
          <ObjectBox key={key} {...b} />
        ))}
      </div>
    </div>
  );
}

// at is where an object was at time t: between two known positions it moves
// linearly; just after the last one it holds briefly; otherwise it is absent.
export function at(pts: TrackPoint[], t: number) {
  if (!pts.length || t < pts[0].ts - 150 || t > pts[pts.length - 1].ts + HOLD) return null;
  let i = 0;
  while (i < pts.length - 1 && pts[i + 1].ts <= t) i++;
  const a = pts[i];
  const b = pts[i + 1];
  if (!b || t <= a.ts || b.ts - a.ts > MAX_GAP) {
    if (t > a.ts + HOLD && b && t < b.ts - 150) return null; // in a gap
    const p = t < a.ts ? a : b && t >= b.ts - 150 && b.ts - a.ts > MAX_GAP ? b : a;
    return { label: p.label, score: p.confidence, x: p.x, y: p.y, w: p.w, h: p.h };
  }
  const f = (t - a.ts) / (b.ts - a.ts);
  const mix = (p: number, q: number) => p + (q - p) * f;
  return {
    label: a.label,
    score: f < 0.5 ? a.confidence : b.confidence,
    x: mix(a.x, b.x),
    y: mix(a.y, b.y),
    w: mix(a.w, b.w),
    h: mix(a.h, b.h),
  };
}
