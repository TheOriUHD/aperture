import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import Hls from "hls.js";
import { ChevronLeft, ChevronRight, Play, Pause, Film } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { Timeline, type TimeRange } from "../components/Timeline";
import { EmptyState } from "../components/ui/EmptyState";
import { api } from "../lib/api";
import { useCameras } from "../hooks/useCameras";
import { wallToOffset, offsetToWall, type Seg } from "../lib/playlist";

interface Segment extends Seg {
  id: string;
  hasMotion: boolean;
}

const DAY_MS = 24 * 60 * 60 * 1000;

function startOfDay(d: Date) {
  const x = new Date(d);
  x.setHours(0, 0, 0, 0);
  return x;
}

function fmtClock(ms: number) {
  const d = new Date(ms);
  return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

export function Recordings() {
  const { data: cameras } = useCameras();
  const [day, setDay] = useState(() => startOfDay(new Date()));
  const [cameraId, setCameraId] = useState<string | null>(null);
  const [playheadMs, setPlayheadMs] = useState<number | undefined>();
  const [playing, setPlaying] = useState(false);

  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);

  const selected = cameras?.find((c) => c.id === cameraId) ?? cameras?.[0] ?? null;
  const activeId = selected?.id ?? null;
  const dayStart = day.getTime();
  const dayEnd = dayStart + DAY_MS;
  const isToday = dayStart === startOfDay(new Date()).getTime();

  const { data: segments = [] } = useQuery({
    queryKey: ["segments", activeId, dayStart],
    queryFn: () => api.get<Segment[]>(`/api/cameras/${activeId}/segments?from=${dayStart}&to=${dayEnd}`),
    enabled: !!activeId,
    refetchInterval: isToday ? 15000 : false,
  });

  const recorded = useMemo<TimeRange[]>(
    () => segments.map((s) => ({ start: s.startTs, end: s.endTs })),
    [segments],
  );
  const motion = useMemo<TimeRange[]>(
    () => segments.filter((s) => s.hasMotion).map((s) => ({ start: s.startTs, end: s.endTs })),
    [segments],
  );

  // Attach (or re-attach) HLS for the selected camera + day.
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !activeId || segments.length === 0) return;
    const url = `/api/cameras/${activeId}/hls?from=${dayStart}&to=${dayEnd}`;

    if (Hls.isSupported()) {
      const hls = new Hls({ maxBufferLength: 30, enableWorker: true });
      hlsRef.current = hls;
      hls.loadSource(url);
      hls.attachMedia(video);
      return () => {
        hls.destroy();
        hlsRef.current = null;
      };
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = url;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeId, dayStart, segments.length > 0]);

  function seekToWall(wallMs: number) {
    const video = videoRef.current;
    if (!video || segments.length === 0) return;
    video.currentTime = wallToOffset(segments, wallMs);
    void video.play().then(() => setPlaying(true)).catch(() => undefined);
  }

  function togglePlay() {
    const video = videoRef.current;
    if (!video) return;
    if (video.paused) {
      void video.play().then(() => setPlaying(true)).catch(() => undefined);
    } else {
      video.pause();
      setPlaying(false);
    }
  }

  return (
    <>
      <TopBar
        title="Recordings"
        right={
          cameras && cameras.length > 0 ? (
            <select
              className="input h-8 w-44"
              value={selected?.id ?? ""}
              onChange={(e) => setCameraId(e.target.value)}
            >
              {cameras.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          ) : undefined
        }
      />
      <div className="flex-1 overflow-auto p-4">
        <div className="max-w-5xl mx-auto space-y-4">
          <div className="relative aspect-video rounded-lg bg-black border border-line overflow-hidden">
            <video
              ref={videoRef}
              playsInline
              className="h-full w-full object-contain bg-black"
              onTimeUpdate={(e) => {
                const wall = offsetToWall(segments, e.currentTarget.currentTime);
                if (wall) setPlayheadMs(wall);
              }}
              onPlay={() => setPlaying(true)}
              onPause={() => setPlaying(false)}
            />
            {!selected && (
              <div className="absolute inset-0 grid place-items-center text-sm text-fg-faint">
                No cameras to play back
              </div>
            )}
            {selected && segments.length === 0 && (
              <div className="absolute inset-0 grid place-items-center">
                <EmptyState icon={Film} title="No footage for this day" />
              </div>
            )}
          </div>

          <div className="flex items-center gap-3">
            <button className="btn-icon" onClick={togglePlay} disabled={segments.length === 0}>
              {playing ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
            </button>
            <span className="text-sm font-mono text-fg tabular-nums w-24">
              {playheadMs ? fmtClock(playheadMs) : "--:--:--"}
            </span>
            <div className="w-px h-5 bg-line" />
            <button className="btn-icon" onClick={() => setDay(new Date(dayStart - DAY_MS))}>
              <ChevronLeft className="h-4 w-4" />
            </button>
            <span className="text-sm font-medium text-fg">
              {day.toLocaleDateString(undefined, { weekday: "short", month: "short", day: "numeric" })}
            </span>
            <button
              className="btn-icon"
              onClick={() => setDay(new Date(dayStart + DAY_MS))}
              disabled={isToday}
            >
              <ChevronRight className="h-4 w-4" />
            </button>
            <div className="ml-auto flex items-center gap-3 text-[11px] text-fg-muted">
              <span className="flex items-center gap-1.5">
                <span className="h-2.5 w-2.5 rounded-sm bg-brand-deep" /> Continuous
              </span>
              <span className="flex items-center gap-1.5">
                <span className="h-2.5 w-2.5 rounded-sm bg-motion" /> Motion
              </span>
            </div>
          </div>

          <Timeline
            dayStart={dayStart}
            segments={recorded}
            events={motion}
            playheadMs={playheadMs}
            onSeek={seekToWall}
          />
        </div>
      </div>
    </>
  );
}
