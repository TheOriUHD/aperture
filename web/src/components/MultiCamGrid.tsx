import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import Hls from "hls.js";
import { X } from "lucide-react";
import { LiveVideo } from "./LiveVideo";
import { api } from "../lib/api";
import { wallToOffset, offsetToWall, type Seg } from "../lib/playlist";
import type { Camera } from "../lib/types";

interface Segment extends Seg {
  id: string;
}
const DAY_MS = 24 * 60 * 60 * 1000;
const startOfDay = (ms: number) => {
  const d = new Date(ms);
  d.setHours(0, 0, 0, 0);
  return d.getTime();
};

// One synced tile: live when following, else its own recorded HLS seeked to the
// shared time. Seeks are debounced so scrubbing doesn't thrash N decoders.
function Tile({
  camera,
  atMs,
  following,
  muted,
  paused,
  quality,
  style,
  onTime,
  onRemove,
}: {
  camera: Camera;
  atMs: number;
  following: boolean;
  muted: boolean;
  paused: boolean;
  quality: "auto" | "low" | "high";
  style?: React.CSSProperties;
  onTime?: (wallMs: number) => void; // set on the primary tile to drive the shared clock
  onRemove?: () => void;
}) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const atRef = useRef(atMs);
  atRef.current = atMs;
  const seekTimer = useRef<number | undefined>(undefined);

  // Honor the shared play/pause for recorded tiles.
  useEffect(() => {
    if (following) return;
    const v = videoRef.current;
    if (!v) return;
    if (paused) v.pause();
    else void v.play().catch(() => undefined);
  }, [paused, following]);

  const dayStart = startOfDay(atMs);
  const dayEnd = dayStart + DAY_MS;
  const { data: segments = [] } = useQuery({
    queryKey: ["segments", camera.id, dayStart],
    queryFn: () => api.get<Segment[]>(`/api/cameras/${camera.id}/segments?from=${dayStart}&to=${dayEnd}`),
    enabled: !following && !!camera.id,
  });
  const segsRef = useRef<Segment[]>(segments);
  segsRef.current = segments;

  // Recorded HLS lifecycle for this tile (only when scrubbed off live).
  useEffect(() => {
    if (following || segments.length === 0) return;
    const video = videoRef.current;
    if (!video) return;
    const url = `/api/v1/cameras/${camera.id}/hls?from=${dayStart}&to=${dayEnd}`;
    if (Hls.isSupported()) {
      const hls = new Hls({ maxBufferLength: 20, enableWorker: true });
      hlsRef.current = hls;
      hls.loadSource(url);
      hls.attachMedia(video);
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        video.currentTime = wallToOffset(segsRef.current, atRef.current);
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
  }, [following, camera.id, dayStart, segments.length > 0]);

  // Seek only when the shared time JUMPS (a scrub) — not when it advances smoothly
  // from playback, which would fight the primary tile driving that same clock.
  useEffect(() => {
    if (following) return;
    window.clearTimeout(seekTimer.current);
    seekTimer.current = window.setTimeout(() => {
      const v = videoRef.current;
      if (!v || !hlsRef.current) return;
      const cur = offsetToWall(segsRef.current, v.currentTime);
      if (cur == null || Math.abs(cur - atRef.current) > 1500) {
        v.currentTime = wallToOffset(segsRef.current, atRef.current);
        void v.play().catch(() => undefined);
      }
    }, 180);
    return () => window.clearTimeout(seekTimer.current);
  }, [atMs, following]);

  return (
    <div className="group relative min-h-0 overflow-hidden bg-black" style={style}>
      {following ? (
        <LiveVideo camera={camera} muted={muted} fit="contain" quality={quality} />
      ) : (
        <video
          ref={videoRef}
          playsInline
          muted={muted}
          className="absolute inset-0 h-full w-full bg-black object-contain"
          onTimeUpdate={(e) => {
            if (!onTime) return; // only the primary tile drives the shared clock
            const wall = offsetToWall(segsRef.current, e.currentTarget.currentTime);
            if (wall) onTime(wall);
          }}
        />
      )}
      <span className="absolute left-2 top-2 rounded bg-black/45 px-2 py-0.5 text-[11px] font-medium text-white backdrop-blur-sm">
        {camera.name}
      </span>
      {onRemove && (
        <button
          onClick={onRemove}
          className="absolute right-2 top-2 hidden h-6 w-6 place-items-center rounded bg-black/50 text-white/90 group-hover:grid hover:bg-black/80"
          title="Remove from view"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  );
}

// A synced multi-camera playback grid. `cameras` are shown together; all follow
// the shared `atMs`/`following` driven by the single timeline.
export function MultiCamGrid({
  cameras,
  atMs,
  following,
  muted,
  paused,
  quality,
  primaryId,
  onTime,
  onRemove,
}: {
  cameras: Camera[];
  atMs: number;
  following: boolean;
  muted: boolean;
  paused: boolean;
  quality: "auto" | "low" | "high";
  primaryId: string | null; // the tile that drives the shared clock in recorded playback
  onTime: (wallMs: number) => void;
  onRemove: (id: string) => void;
}) {
  const n = cameras.length;
  const { style: gridStyle, cellStyle } = gridLayout(n);
  return (
    <div className="absolute inset-0 grid gap-0.5 bg-ink-900" style={gridStyle}>
      {cameras.map((c, i) => (
        <Tile
          key={c.id}
          camera={c}
          atMs={atMs}
          following={following}
          muted={muted}
          paused={paused}
          quality={quality}
          style={cellStyle(i)}
          onTime={c.id === primaryId ? onTime : undefined}
          onRemove={n > 1 ? () => onRemove(c.id) : undefined}
        />
      ))}
    </div>
  );
}

// gridLayout returns the CSS grid config for n cameras, shared by the live grid
// and the picker's layout preview so they always match:
//   1 = full, 2 = side by side, 3 = two on top + one centered below, 4 = 2×2,
//   5+ = 3 columns.
export function gridLayout(n: number): {
  style: React.CSSProperties;
  cellStyle: (i: number) => React.CSSProperties | undefined;
} {
  const cols = n <= 1 ? 1 : n <= 4 ? 2 : 3;
  const three = n === 3;
  return {
    style: {
      gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`,
      gridTemplateRows: three ? "repeat(2, minmax(0, 1fr))" : undefined,
    },
    cellStyle: (i: number) =>
      three && i === 2 ? { gridColumn: "1 / -1", justifySelf: "center", width: "calc(50% - 1px)" } : undefined,
  };
}
