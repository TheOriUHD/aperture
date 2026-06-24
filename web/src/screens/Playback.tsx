import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import Hls from "hls.js";
import {
  Play,
  Pause,
  Rewind,
  FastForward,
  Maximize2,
  Camera as CameraIcon,
  ChevronLeft,
  ChevronRight,
  ArrowUp,
  LayoutGrid,
  Calendar,
  Cctv,
  Scissors,
} from "lucide-react";
import { CameraRail } from "../components/CameraRail";
import { VerticalTimeline, type TR, type TLEvent } from "../components/VerticalTimeline";
import { EventStrip } from "../components/EventStrip";
import { LiveVideo } from "../components/LiveVideo";
import { ZoomControl } from "../components/ZoomControl";
import { ExportCenter } from "../components/ExportCenter";
import { useCameras } from "../hooks/useCameras";
import { useMediaQuery } from "../hooks/useMediaQuery";
import { usePrefs } from "../lib/prefs";
import { api } from "../lib/api";
import { wallToOffset, offsetToWall, mergeRanges, type Seg } from "../lib/playlist";
import type { MotionEvent } from "../lib/types";

interface Segment extends Seg {
  id: string;
  hasMotion: boolean;
}

const DAY_MS = 24 * 60 * 60 * 1000;
const MONTH_MS = 30 * DAY_MS;
// Largest window first so the zoom slider reads left = out, right (+) = in.
const ZOOMS = [720, 360, 240, 180, 120, 90, 60, 45, 30, 20, 15, 10, 5].map((m) => m * 60_000);
const ZOOM_DEFAULT = 8; // 30 min

const startOfDay = (ms: number) => {
  const d = new Date(ms);
  d.setHours(0, 0, 0, 0);
  return d.getTime();
};
const clamp = (x: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, x));
const fmtClock = (ms: number) =>
  new Date(ms).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" });
const fmtDate = (ms: number) =>
  new Date(ms).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });

export function Playback() {
  const { data: cameras } = useCameras();
  const [params] = useSearchParams();
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const [nowMs, setNowMs] = useState(() => Date.now());
  const [following, setFollowing] = useState(true);
  const [selectedState, setSelectedState] = useState(() => Date.now());
  const { prefs } = usePrefs();
  const [zoomIdx, setZoomIdx] = useState(
    Math.min(ZOOMS.length - 1, Math.max(0, prefs.timelineZoom ?? ZOOM_DEFAULT)),
  );
  const [paused, setPaused] = useState(false);

  const isDesktop = useMediaQuery("(min-width: 1024px)");
  const [camerasOpen, setCamerasOpen] = useState(false);
  const [exportOpen, setExportOpen] = useState(false);

  const playerVideoRef = useRef<HTMLVideoElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const dateInputRef = useRef<HTMLInputElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const selectedRef = useRef<number>(selectedState);
  const segmentsRef = useRef<Segment[]>([]);
  const seekTimer = useRef<number | undefined>(undefined);

  const windowMs = ZOOMS[zoomIdx];
  const isLive = following;
  const selectedMs = following ? nowMs : selectedState;
  selectedRef.current = selectedMs;
  const dayStart = startOfDay(selectedMs);
  const dayEnd = dayStart + DAY_MS;

  useEffect(() => {
    const i = setInterval(() => setNowMs(Date.now()), 1000);
    return () => clearInterval(i);
  }, []);

  useEffect(() => {
    if (!cameras?.length) return;
    const q = params.get("camera");
    if (q && cameras.some((c) => c.id === q)) setSelectedId(q);
    else setSelectedId((prev) => prev ?? cameras[0].id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cameras]);

  // Deep link: ?t=<unix ms> opens playback at that moment (e.g. from Alarms).
  const didInitT = useRef(false);
  useEffect(() => {
    if (didInitT.current) return;
    const t = Number(params.get("t"));
    if (t > 0) {
      didInitT.current = true;
      setFollowing(false);
      setSelectedState(t);
      selectedRef.current = t;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params]);

  const selected = cameras?.find((c) => c.id === selectedId) ?? null;

  const followingToday = dayStart === startOfDay(nowMs);

  const { data: segments = [] } = useQuery({
    queryKey: ["segments", selectedId, dayStart],
    queryFn: () => api.get<Segment[]>(`/api/cameras/${selectedId}/segments?from=${dayStart}&to=${dayEnd}`),
    enabled: !!selectedId,
    refetchInterval: followingToday ? 15000 : false,
  });
  segmentsRef.current = segments;

  const { data: events = [] } = useQuery({
    queryKey: ["events", selectedId, dayStart],
    queryFn: () => api.get<MotionEvent[]>(`/api/cameras/${selectedId}/events?from=${dayStart}&to=${dayEnd}`),
    enabled: !!selectedId,
    refetchInterval: followingToday ? 15000 : false,
  });

  const recorded: TR[] = mergeRanges(segments);
  const tlEvents: TLEvent[] = events.map((e) => ({
    id: e.id,
    start: e.startTs,
    end: e.endTs,
    thumbUrl: e.thumbUrl,
  }));

  // HLS playback for the active day.
  useEffect(() => {
    if (isLive || !selected || segments.length === 0) return;
    const video = playerVideoRef.current;
    if (!video) return;
    const url = `/api/cameras/${selected.id}/hls?from=${dayStart}&to=${dayEnd}`;
    if (Hls.isSupported()) {
      const hls = new Hls({ maxBufferLength: 30, enableWorker: true });
      hlsRef.current = hls;
      hls.loadSource(url);
      hls.attachMedia(video);
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        video.currentTime = wallToOffset(segmentsRef.current, selectedRef.current);
        void video.play().catch(() => undefined);
      });
      return () => {
        hls.destroy();
        hlsRef.current = null;
      };
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = url;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isLive, selected?.id, dayStart, segments.length > 0]);

  const scrubbing = useRef(false);
  const snapAnim = useRef(0);
  const allowSnap = useRef(true);
  const nowRef = useRef(nowMs);
  nowRef.current = nowMs;

  // Smoothly ease the view to the live edge, then switch to live.
  function animateToLive() {
    cancelAnimationFrame(snapAnim.current);
    scrubbing.current = true;
    const from = selectedRef.current;
    const startT = performance.now();
    const dur = 320;
    const step = (t: number) => {
      const k = Math.min(1, (t - startT) / dur);
      const e = 1 - Math.pow(1 - k, 3); // ease-out cubic
      const val = from + (nowRef.current - from) * e;
      setSelectedState(val);
      selectedRef.current = val;
      if (k < 1) {
        snapAnim.current = requestAnimationFrame(step);
      } else {
        scrubbing.current = false;
        setFollowing(true);
        setPaused(false);
      }
    };
    snapAnim.current = requestAnimationFrame(step);
  }

  // After a scrub settles, either snap (smoothly) to live or seek the player.
  function scheduleSettle() {
    window.clearTimeout(seekTimer.current);
    seekTimer.current = window.setTimeout(() => {
      const snapMs = clamp(windowMs * 0.04, 5000, 90_000);
      if (allowSnap.current && selectedRef.current >= nowRef.current - snapMs) {
        animateToLive();
        return;
      }
      scrubbing.current = false;
      const v = playerVideoRef.current;
      if (v && hlsRef.current) {
        v.currentTime = wallToOffset(segmentsRef.current, selectedRef.current);
        void v.play().catch(() => undefined);
      }
    }, 150);
  }

  function applyScrub(ms: number, snap: boolean) {
    cancelAnimationFrame(snapAnim.current);
    allowSnap.current = snap;
    const c = clamp(ms, nowMs - MONTH_MS, nowMs);
    scrubbing.current = true;
    setFollowing(false);
    setSelectedState(c);
    selectedRef.current = c;
    scheduleSettle();
  }

  // Drag / wheel scrubbing: snaps to live when settling near the edge.
  function scrubToTime(ms: number) {
    applyScrub(ms, true);
  }
  // Explicit seeks (skip buttons, timeline click): never snap; only go live if
  // the target is essentially the present.
  function seekToTime(ms: number) {
    if (ms >= nowMs - 1500) {
      goLive();
      return;
    }
    applyScrub(ms, false);
  }

  function shiftDay(dir: number) {
    seekToTime(selectedMs + dir * DAY_MS);
  }
  function goLive() {
    if (!following) animateToLive();
  }

  function togglePlay() {
    const v = playerVideoRef.current;
    if (!v) return;
    if (v.paused) void v.play().then(() => setPaused(false)).catch(() => undefined);
    else {
      v.pause();
      setPaused(true);
    }
  }
  function snapshot() {
    const v = playerVideoRef.current;
    if (!v || !v.videoWidth) return;
    const canvas = document.createElement("canvas");
    canvas.width = v.videoWidth;
    canvas.height = v.videoHeight;
    canvas.getContext("2d")?.drawImage(v, 0, 0);
    canvas.toBlob(
      (b) => {
        if (!b) return;
        const url = URL.createObjectURL(b);
        const a = document.createElement("a");
        a.href = url;
        a.download = `${selected?.name ?? "camera"}-${new Date().toISOString().replace(/[:.]/g, "-")}.jpg`;
        a.click();
        URL.revokeObjectURL(url);
      },
      "image/jpeg",
      0.95,
    );
  }
  function fullscreen() {
    if (!document.fullscreenElement) void containerRef.current?.requestFullscreen?.();
    else void document.exitFullscreen();
  }

  function openDatePicker() {
    dateInputRef.current?.showPicker?.();
    dateInputRef.current?.click();
  }

  // One video element, reused by both layouts (only one renders at a time).
  function renderPlayer() {
    if (!selected) {
      return <div className="absolute inset-0 grid place-items-center text-sm text-fg-faint">No cameras</div>;
    }
    return isLive ? (
      <LiveVideo key={selected.id} camera={selected} fit="contain" videoRef={playerVideoRef} />
    ) : (
      <video
        ref={playerVideoRef}
        playsInline
        className="absolute inset-0 h-full w-full object-contain bg-black"
        onTimeUpdate={(e) => {
          if (scrubbing.current) return;
          const wall = offsetToWall(segmentsRef.current, e.currentTarget.currentTime);
          if (wall) {
            setSelectedState(wall);
            selectedRef.current = wall;
          }
        }}
        onPlay={() => setPaused(false)}
        onPause={() => setPaused(true)}
      />
    );
  }

  const overlayTime = selectedMs;
  const atToday = dayStart >= startOfDay(nowMs);
  const fmtDay = (ms: number) => new Date(ms).toLocaleDateString(undefined, { month: "short", day: "numeric" });

  const hiddenDateInput = (
    <input
      ref={dateInputRef}
      type="date"
      className="sr-only"
      onChange={(e) => {
        if (!e.target.value) return;
        seekToTime(new Date(e.target.value + "T12:00").getTime());
      }}
    />
  );

  // ---- Mobile layout (UniFi Protect-style) --------------------------------
  const exportModal = (
    <ExportCenter
      open={exportOpen}
      onClose={() => setExportOpen(false)}
      camera={selected}
      cameras={cameras ?? []}
      defaultTo={selectedMs}
    />
  );

  if (!isDesktop) {
    return (
      <div className="flex h-full min-h-0 flex-col bg-ink-900">
        {exportModal}
        {/* camera switcher (slides in/out) */}
        <div
          className={`shrink-0 overflow-hidden border-b border-line transition-[max-height] duration-300 ${
            camerasOpen ? "max-h-[120px]" : "max-h-0"
          }`}
        >
          <div className="flex gap-2 overflow-x-auto p-2">
            {(cameras ?? []).map((c) => (
              <button
                key={c.id}
                onClick={() => setSelectedId(c.id)}
                className="shrink-0 w-[120px]"
              >
                <div
                  className={`relative aspect-video rounded-lg overflow-hidden border ${
                    c.id === selectedId ? "border-brand ring-1 ring-brand" : "border-line"
                  }`}
                >
                  <MobileThumb cameraId={c.id} enabled={c.enabled} />
                </div>
                <p
                  className={`mt-1 text-[11px] uppercase tracking-wide truncate ${
                    c.id === selectedId ? "text-fg font-medium" : "text-fg-muted"
                  }`}
                >
                  {c.name}
                </p>
              </button>
            ))}
          </div>
        </div>

        {/* video */}
        <div ref={containerRef} className="relative shrink-0 aspect-video bg-black">
          {renderPlayer()}
          {selected && (
            <div className="absolute top-2 left-2 rounded-md bg-black/40 px-2 py-1 text-[11px] font-mono text-white/90 backdrop-blur-sm">
              {fmtDate(overlayTime)} {fmtClock(overlayTime)} · {selected.name}
            </div>
          )}
        </div>

        {/* control header */}
        {selected && (
          <div className="flex items-center gap-2 px-3 h-12 border-b border-line shrink-0">
            <span className="flex items-center gap-2 text-sm font-medium text-fg">
              <span className={`h-2.5 w-2.5 rounded-full ${isLive ? "bg-rec" : "bg-fg-faint"}`} />
              {selected.name}
            </span>
            <div className="ml-auto flex items-center gap-0.5">
              {!isLive && (
                <>
                  <BarBtn onClick={togglePlay} label={paused ? "Play" : "Pause"}>
                    {paused ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}
                  </BarBtn>
                  <BarBtn onClick={() => seekToTime(selectedMs - 30_000)} label="Back 30 seconds">
                    <Rewind className="h-4 w-4" />
                  </BarBtn>
                </>
              )}
              <BarBtn onClick={() => setCamerasOpen((o) => !o)} label="Cameras">
                <LayoutGrid className="h-4 w-4" />
              </BarBtn>
              <BarBtn onClick={() => setExportOpen(true)} label="Export clip">
                <Scissors className="h-4 w-4" />
              </BarBtn>
              <BarBtn onClick={fullscreen} label="Fullscreen">
                <Maximize2 className="h-4 w-4" />
              </BarBtn>
            </div>
          </div>
        )}

        {/* timeline */}
        <div className="relative flex-1 min-h-0">
          {selected && (
            <VerticalTimeline
              selectedMs={selectedMs}
              windowMs={windowMs}
              nowMs={nowMs}
              segments={recorded}
              events={tlEvents}
              live={following}
              onScrubTo={scrubToTime}
              onSeekAbs={seekToTime}
              onEventClick={seekToTime}
            />
          )}
          <ZoomControl
            value={zoomIdx}
            max={ZOOMS.length - 1}
            onChange={setZoomIdx}
            className="absolute top-3 right-3"
          />
          {!following && (
            <button
              onClick={goLive}
              className="absolute bottom-16 right-3 h-10 w-10 rounded-full bg-brand text-white grid place-items-center shadow-pop"
              aria-label="Jump to live"
            >
              <ArrowUp className="h-4 w-4" />
            </button>
          )}
          <button
            onClick={openDatePicker}
            className="absolute bottom-3 left-3 flex items-center gap-2 rounded-full bg-ink-700/90 px-3.5 py-2.5 text-xs text-fg backdrop-blur-sm"
          >
            <Calendar className="h-3.5 w-3.5" />
            {fmtDay(selectedMs)}
          </button>
          <button
            onClick={snapshot}
            className="absolute bottom-3 right-3 h-10 w-10 rounded-full bg-ink-700/90 text-fg-muted grid place-items-center backdrop-blur-sm"
            aria-label="Snapshot"
          >
            <CameraIcon className="h-4 w-4" />
          </button>
        </div>
        {hiddenDateInput}
      </div>
    );
  }

  // ---- Desktop layout -----------------------------------------------------
  return (
    <div className="flex h-full min-h-0">
      {exportModal}
      <CameraRail cameras={cameras ?? []} selectedId={selectedId} onSelect={setSelectedId} />

      {/* timeline column */}
      <div className="w-[208px] shrink-0 border-r border-line flex flex-col bg-ink-900">
        <div className="px-3 pt-3 pb-2.5 border-b border-line space-y-2.5">
          <div className="flex items-center justify-between">
            <button className="btn-icon h-6 w-6" onClick={() => shiftDay(-1)} aria-label="Previous day">
              <ChevronLeft className="h-4 w-4" />
            </button>
            <span className="text-xs font-medium text-fg">{fmtDate(selectedMs)}</span>
            <button
              className="btn-icon h-6 w-6"
              onClick={() => shiftDay(1)}
              disabled={atToday}
              aria-label="Next day"
            >
              <ChevronRight className="h-4 w-4" />
            </button>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-sm text-fg-faint leading-none">−</span>
            <input
              type="range"
              min={0}
              max={ZOOMS.length - 1}
              step={1}
              value={zoomIdx}
              onChange={(e) => setZoomIdx(Number(e.target.value))}
              className="flex-1 min-w-0"
            />
            <span className="text-sm text-fg-faint leading-none">+</span>
          </div>
        </div>

        <div className="relative flex-1 min-h-0 py-2">
          <VerticalTimeline
            selectedMs={selectedMs}
            windowMs={windowMs}
            nowMs={nowMs}
            segments={recorded}
            events={tlEvents}
            live={following}
            onScrubTo={scrubToTime}
            onSeekAbs={seekToTime}
            onEventClick={seekToTime}
          />
          {!following && (
            <button
              onClick={goLive}
              className="absolute bottom-3 right-3 h-9 w-9 rounded-full bg-brand text-white grid place-items-center shadow-pop hover:bg-[#2aa6f5]"
              aria-label="Jump to live"
            >
              <ArrowUp className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>

      {/* player + control bar + events */}
      <div className="flex-1 min-w-0 flex flex-col">
        <div ref={containerRef} className="relative flex-1 bg-black min-h-0">
          {renderPlayer()}

          {selected && (
            <div className="absolute top-3 left-3 flex items-center gap-2 rounded-md bg-black/35 px-2.5 py-1 backdrop-blur-sm">
              <span className="text-xs font-mono tabular-nums text-white/90">{fmtClock(overlayTime)}</span>
              <span className="text-white/30">|</span>
              <span className="text-xs font-medium text-white">{selected.name}</span>
            </div>
          )}
        </div>

        {/* control bar (below the video) */}
        {selected && (
          <div className="flex items-center gap-1 h-12 px-3 border-t border-line bg-ink-900 shrink-0">
            {isLive ? (
              <span className="pill bg-online/20 text-online mr-1">
                <span className="h-1.5 w-1.5 rounded-full bg-online" />
                LIVE
              </span>
            ) : (
              <BarBtn onClick={togglePlay} label={paused ? "Play" : "Pause"}>
                {paused ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}
              </BarBtn>
            )}
            <BarBtn onClick={() => seekToTime((isLive ? nowMs : selectedMs) - 30_000)} label="Back 30 seconds">
              <Rewind className="h-4 w-4" />
            </BarBtn>
            {!isLive && (
              <BarBtn onClick={() => seekToTime(selectedMs + 30_000)} label="Forward 30 seconds">
                <FastForward className="h-4 w-4" />
              </BarBtn>
            )}
            {!isLive && (
              <button onClick={goLive} className="ml-1 h-8 rounded-md px-2.5 text-xs text-fg-muted hover:text-fg hover:bg-ink-700">
                Go live
              </button>
            )}
            <span className="ml-2 text-xs font-mono text-fg-muted tabular-nums">{fmtClock(overlayTime)}</span>
            <div className="ml-auto flex items-center gap-0.5">
              <BarBtn onClick={snapshot} label="Snapshot">
                <CameraIcon className="h-4 w-4" />
              </BarBtn>
              <BarBtn onClick={() => setExportOpen(true)} label="Export clip">
                <Scissors className="h-4 w-4" />
              </BarBtn>
              <BarBtn onClick={fullscreen} label="Fullscreen">
                <Maximize2 className="h-4 w-4" />
              </BarBtn>
            </div>
          </div>
        )}

        <EventStrip
          dateLabel={fmtDate(selectedMs)}
          events={events}
          onSelect={(ms) => seekToTime(ms)}
        />
      </div>
    </div>
  );
}

function BarBtn({
  onClick,
  label,
  children,
}: {
  onClick: () => void;
  label: string;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      aria-label={label}
      className="h-8 w-8 grid place-items-center rounded-md text-fg-muted hover:text-fg hover:bg-ink-700"
    >
      {children}
    </button>
  );
}

function MobileThumb({ cameraId, enabled }: { cameraId: string; enabled: boolean }) {
  const [tick, setTick] = useState(0);
  const [ok, setOk] = useState(true);
  useEffect(() => {
    if (!enabled) return;
    const i = setInterval(() => setTick((t) => t + 1), 5000);
    return () => clearInterval(i);
  }, [enabled]);
  if (!enabled || !ok) {
    return (
      <div className="absolute inset-0 grid place-items-center bg-ink-800">
        <Cctv className="h-5 w-5 text-fg-faint" />
      </div>
    );
  }
  return (
    <img
      src={`/api/cameras/${cameraId}/snapshot.jpg?t=${tick}`}
      alt=""
      className="h-full w-full object-cover"
      onError={() => setOk(false)}
    />
  );
}
