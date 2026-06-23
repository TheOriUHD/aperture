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
  Radio,
} from "lucide-react";
import { CameraRail } from "../components/CameraRail";
import { VerticalTimeline, type TR } from "../components/VerticalTimeline";
import { EventStrip } from "../components/EventStrip";
import { LiveVideo } from "../components/LiveVideo";
import { useCameras } from "../hooks/useCameras";
import { api } from "../lib/api";
import { wallToOffset, offsetToWall, type Seg } from "../lib/playlist";

interface Segment extends Seg {
  id: string;
  hasMotion: boolean;
}

const DAY_MS = 24 * 60 * 60 * 1000;
const ZOOMS = [10, 30, 60, 180, 360, 720].map((m) => m * 60_000);

const startOfDay = (ms: number) => {
  const d = new Date(ms);
  d.setHours(0, 0, 0, 0);
  return d.getTime();
};
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
  const [viewTopFrozen, setViewTopFrozen] = useState<number | null>(null);
  const [playheadMs, setPlayheadMs] = useState<number | undefined>();
  const [zoomIdx, setZoomIdx] = useState(1);
  const [paused, setPaused] = useState(false);

  const playerVideoRef = useRef<HTMLVideoElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const playheadRef = useRef<number | undefined>(undefined);
  playheadRef.current = playheadMs;

  const windowMs = ZOOMS[zoomIdx];
  const isLive = playheadMs === undefined; // player shows live when no playhead
  const viewTopMs = following ? nowMs : viewTopFrozen ?? nowMs;
  const dayStart = startOfDay(viewTopMs);
  const dayEnd = dayStart + DAY_MS;

  // live clock
  useEffect(() => {
    const i = setInterval(() => setNowMs(Date.now()), 1000);
    return () => clearInterval(i);
  }, []);

  // initial / deep-link camera selection
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

  const recorded: TR[] = segments.map((s) => ({ start: s.startTs, end: s.endTs }));
  const motion: TR[] = segments.filter((s) => s.hasMotion).map((s) => ({ start: s.startTs, end: s.endTs }));

  // Attach HLS in playback mode for the active day.
  useEffect(() => {
    if (isLive || !selected || segments.length === 0) return;
    const video = playerVideoRef.current;
    if (!video) return;
    const url = `/api/cameras/${selected.id}/hls?from=${dayStart}&to=${dayEnd}`;
    const segs = segments;
    if (Hls.isSupported()) {
      const hls = new Hls({ maxBufferLength: 30, enableWorker: true });
      hlsRef.current = hls;
      hls.loadSource(url);
      hls.attachMedia(video);
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        if (playheadRef.current !== undefined) {
          video.currentTime = wallToOffset(segs, playheadRef.current);
          void video.play().catch(() => undefined);
        }
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

  function goLive() {
    setFollowing(true);
    setPlayheadMs(undefined);
    setPaused(false);
  }

  function seekTo(wallMs: number) {
    if (segments.length === 0) return;
    const clamped = Math.min(wallMs, nowMs);
    setFollowing(false);
    setViewTopFrozen(viewTopMs);
    setPlayheadMs(clamped);
    setPaused(false);
    const video = playerVideoRef.current;
    if (video && hlsRef.current) {
      video.currentTime = wallToOffset(segments, clamped);
      void video.play().catch(() => undefined);
    }
  }

  function pan(deltaMs: number) {
    const base = following ? nowMs : viewTopFrozen ?? nowMs;
    setFollowing(false);
    setViewTopFrozen(Math.min(nowMs, base - deltaMs));
  }

  function shiftDay(dir: number) {
    const base = following ? nowMs : viewTopFrozen ?? nowMs;
    setFollowing(false);
    setViewTopFrozen(Math.min(nowMs, base + dir * DAY_MS));
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

  const overlayTime = isLive ? nowMs : playheadMs ?? viewTopMs;

  return (
    <div className="flex h-full min-h-0">
      <CameraRail cameras={cameras ?? []} selectedId={selectedId} onSelect={setSelectedId} />

      {/* timeline column */}
      <div className="w-[112px] shrink-0 border-r border-line flex flex-col bg-ink-900">
        <div className="flex items-center justify-between px-2 pt-2.5 pb-2 border-b border-line">
          <button className="btn-icon h-6 w-6" onClick={() => shiftDay(-1)}>
            <ChevronLeft className="h-3.5 w-3.5" />
          </button>
          <span className="text-[11px] font-medium text-fg-muted">{fmtDate(viewTopMs)}</span>
          <button
            className="btn-icon h-6 w-6"
            onClick={() => shiftDay(1)}
            disabled={dayStart >= startOfDay(nowMs)}
          >
            <ChevronRight className="h-3.5 w-3.5" />
          </button>
        </div>
        <div className="flex items-center gap-2 px-2 py-2 border-b border-line">
          <span className="text-[10px] text-fg-faint">−</span>
          <input
            type="range"
            min={0}
            max={ZOOMS.length - 1}
            step={1}
            value={ZOOMS.length - 1 - zoomIdx}
            onChange={(e) => setZoomIdx(ZOOMS.length - 1 - Number(e.target.value))}
            className="flex-1"
          />
          <span className="text-[10px] text-fg-faint">+</span>
        </div>
        <button
          onClick={goLive}
          className={`mx-2 my-2 h-7 rounded-md text-xs font-medium flex items-center justify-center gap-1.5 ${
            following && isLive ? "bg-brand text-white" : "bg-ink-700 text-fg-muted hover:text-fg"
          }`}
        >
          <Radio className="h-3.5 w-3.5" />
          LIVE
        </button>
        <div className="flex-1 min-h-0 px-1 pb-2">
          <VerticalTimeline
            topMs={viewTopMs}
            windowMs={windowMs}
            nowMs={nowMs}
            segments={recorded}
            events={motion}
            playheadMs={playheadMs}
            onSeek={seekTo}
            onPan={pan}
          />
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
                  const wall = offsetToWall(segments, e.currentTarget.currentTime);
                  if (wall) {
                    setPlayheadMs(wall);
                    if (!e.currentTarget.paused) {
                      setViewTopFrozen(Math.min(nowMs, wall + windowMs / 2));
                    }
                  }
                }}
                onPlay={() => setPaused(false)}
                onPause={() => setPaused(true)}
              />
            )
          ) : (
            <div className="absolute inset-0 grid place-items-center text-sm text-fg-faint">
              No cameras
            </div>
          )}

          {/* top overlay */}
          {selected && (
            <div className="absolute top-3 left-4 flex items-center gap-2 text-white drop-shadow">
              <span className="text-sm font-mono tabular-nums">
                {fmtDate(overlayTime)} {fmtClock(overlayTime)}
              </span>
              <span className="text-fg-faint">|</span>
              <span className="text-sm font-medium">{selected.name}</span>
            </div>
          )}

          {/* transport controls */}
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
                    <CtlBtn onClick={() => playheadMs && seekTo(playheadMs - 30_000)} label="Back 30s">
                      <Rewind className="h-4 w-4" />
                    </CtlBtn>
                    <button onClick={goLive} className="pill bg-black/55 text-white/90 hover:bg-black/75">
                      Go live
                    </button>
                  </>
                )}
                <span className="text-xs font-mono text-white/90 tabular-nums ml-1">
                  {fmtClock(overlayTime)}
                </span>
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

        <EventStrip dateLabel={fmtDate(viewTopMs)} />
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
