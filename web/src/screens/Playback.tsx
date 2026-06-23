import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import Hls from "hls.js";
import {
  Play,
  Pause,
  Rewind,
  Maximize2,
  Camera as CameraIcon,
  ChevronLeft,
  ChevronRight,
  ArrowUp,
} from "lucide-react";
import { CameraRail } from "../components/CameraRail";
import { VerticalTimeline, type TR } from "../components/VerticalTimeline";
import { EventStrip } from "../components/EventStrip";
import { LiveVideo } from "../components/LiveVideo";
import { useCameras } from "../hooks/useCameras";
import { api } from "../lib/api";
import { wallToOffset, offsetToWall, mergeRanges, type Seg } from "../lib/playlist";

interface Segment extends Seg {
  id: string;
  hasMotion: boolean;
}

const DAY_MS = 24 * 60 * 60 * 1000;
const MONTH_MS = 30 * DAY_MS;
// Largest window first so the zoom slider reads left = out, right (+) = in.
const ZOOMS = [720, 360, 180, 60, 30, 10].map((m) => m * 60_000);

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
  const [zoomIdx, setZoomIdx] = useState(4); // 30 min
  const [paused, setPaused] = useState(false);

  const playerVideoRef = useRef<HTMLVideoElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
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

  const selected = cameras?.find((c) => c.id === selectedId) ?? null;

  const { data: segments = [] } = useQuery({
    queryKey: ["segments", selectedId, dayStart],
    queryFn: () => api.get<Segment[]>(`/api/cameras/${selectedId}/segments?from=${dayStart}&to=${dayEnd}`),
    enabled: !!selectedId,
    refetchInterval: dayStart === startOfDay(nowMs) ? 15000 : false,
  });
  segmentsRef.current = segments;

  const recorded: TR[] = mergeRanges(segments);
  const motion: TR[] = mergeRanges(segments.filter((s) => s.hasMotion));

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

  function requestSeek() {
    window.clearTimeout(seekTimer.current);
    seekTimer.current = window.setTimeout(() => {
      const v = playerVideoRef.current;
      if (v && hlsRef.current) {
        v.currentTime = wallToOffset(segmentsRef.current, selectedRef.current);
        void v.play().catch(() => undefined);
      }
    }, 110);
  }

  function setSelected(ms: number) {
    const c = clamp(ms, nowMs - MONTH_MS, nowMs);
    setFollowing(false);
    setSelectedState(c);
    selectedRef.current = c;
  }

  function onScrub(deltaMs: number) {
    setSelected(selectedMs + deltaMs);
    requestSeek();
  }
  function onSeekTo(ms: number) {
    setSelected(ms);
    setPaused(false);
    requestSeek();
  }
  function shiftDay(dir: number) {
    setSelected(selectedMs + dir * DAY_MS);
    requestSeek();
  }
  function goLive() {
    setFollowing(true);
    setPaused(false);
    selectedRef.current = nowMs;
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

  const overlayTime = selectedMs;
  const atToday = dayStart >= startOfDay(nowMs);

  return (
    <div className="flex h-full min-h-0">
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
            events={motion}
            following={following}
            onScrub={onScrub}
            onSeekTo={onSeekTo}
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

      {/* player + events */}
      <div className="flex-1 min-w-0 flex flex-col">
        <div ref={containerRef} className="flex-1 relative bg-black min-h-0">
          {selected ? (
            isLive ? (
              <LiveVideo
                key={selected.id}
                camera={selected}
                fit="contain"
                videoRef={playerVideoRef}
                className="absolute inset-0"
              />
            ) : (
              <video
                ref={playerVideoRef}
                playsInline
                className="absolute inset-0 h-full w-full object-contain bg-black"
                onTimeUpdate={(e) => {
                  const wall = offsetToWall(segmentsRef.current, e.currentTarget.currentTime);
                  if (wall) {
                    setSelectedState(wall);
                    selectedRef.current = wall;
                  }
                }}
                onPlay={() => setPaused(false)}
                onPause={() => setPaused(true)}
              />
            )
          ) : (
            <div className="absolute inset-0 grid place-items-center text-sm text-fg-faint">No cameras</div>
          )}

          {selected && (
            <div className="absolute top-3 left-4 flex items-center gap-2 text-white drop-shadow">
              <span className="text-sm font-mono tabular-nums">
                {fmtDate(overlayTime)} {fmtClock(overlayTime)}
              </span>
              <span className="text-fg-faint">|</span>
              <span className="text-sm font-medium">{selected.name}</span>
            </div>
          )}

          {selected && (
            <>
              <div className="absolute bottom-0 inset-x-0 h-16 bg-gradient-to-t from-black/70 to-transparent pointer-events-none" />
              <div className="absolute bottom-3 inset-x-4 flex items-center gap-2">
                {isLive ? (
                  <span className="pill bg-online/20 text-online">
                    <span className="h-1.5 w-1.5 rounded-full bg-online" />
                    LIVE
                  </span>
                ) : (
                  <>
                    <CtlBtn onClick={togglePlay} label={paused ? "Play" : "Pause"}>
                      {paused ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}
                    </CtlBtn>
                    <CtlBtn onClick={() => onSeekTo(selectedMs - 30_000)} label="Back 30s">
                      <Rewind className="h-4 w-4" />
                    </CtlBtn>
                    <button onClick={goLive} className="pill bg-black/55 text-white/90 hover:bg-black/75">
                      Go live
                    </button>
                  </>
                )}
                <span className="text-xs font-mono text-white/90 tabular-nums ml-1">{fmtClock(overlayTime)}</span>
                <div className="ml-auto flex items-center gap-1.5">
                  <CtlBtn onClick={snapshot} label="Snapshot">
                    <CameraIcon className="h-4 w-4" />
                  </CtlBtn>
                  <CtlBtn onClick={fullscreen} label="Fullscreen">
                    <Maximize2 className="h-4 w-4" />
                  </CtlBtn>
                </div>
              </div>
            </>
          )}
        </div>

        <EventStrip dateLabel={fmtDate(selectedMs)} />
      </div>
    </div>
  );
}

function CtlBtn({
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
      className="h-9 w-9 grid place-items-center rounded-md bg-black/55 text-white/90 hover:bg-black/75"
    >
      {children}
    </button>
  );
}
