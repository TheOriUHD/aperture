import { Activity, Footprints } from "lucide-react";
import type { MotionEvent } from "../lib/types";

const fmtTime = (ms: number) =>
  new Date(ms).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });

// UniFi-style Playback footer: motion event thumbnails for the day; click to
// jump the player to that moment.
export function EventStrip({
  dateLabel,
  events,
  onSelect,
}: {
  dateLabel: string;
  events: MotionEvent[];
  onSelect: (startMs: number) => void;
}) {
  const ordered = [...events].sort((a, b) => b.startTs - a.startTs);

  return (
    <div className="border-t border-line bg-ink-900 shrink-0">
      <div className="flex items-center gap-3 px-4 h-11 border-b border-line">
        <span className="text-xs font-medium text-fg">{dateLabel}</span>
        <div className="w-px h-4 bg-line" />
        <div className="flex items-center gap-1 text-xs">
          <Chip active>All</Chip>
          <Chip>Motion</Chip>
        </div>
        <span className="ml-auto text-xs text-fg-faint">{ordered.length} detections</span>
      </div>
      {ordered.length === 0 ? (
        <div className="h-[108px] grid place-items-center">
          <div className="flex flex-col items-center gap-1.5 text-fg-faint">
            <Activity className="h-5 w-5" />
            <span className="text-xs">No motion detected for this day</span>
          </div>
        </div>
      ) : (
        <div className="h-[108px] flex items-center gap-2 px-3 overflow-x-auto">
          {ordered.map((e) => (
            <button
              key={e.id}
              onClick={() => onSelect(e.startTs)}
              className="group relative shrink-0 w-[132px] aspect-video rounded-md overflow-hidden border border-line hover:border-motion bg-ink-800"
            >
              {e.thumbUrl ? (
                <img src={e.thumbUrl} alt="" className="h-full w-full object-cover" />
              ) : (
                <div className="h-full w-full grid place-items-center text-fg-faint">
                  <Footprints className="h-5 w-5" />
                </div>
              )}
              <div className="absolute inset-x-0 bottom-0 h-7 bg-gradient-to-t from-black/70 to-transparent" />
              <div className="absolute bottom-1 left-1.5 flex items-center gap-1">
                <Footprints className="h-3 w-3 text-motion" />
                <span className="text-[11px] font-medium text-white">{fmtTime(e.startTs)}</span>
              </div>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function Chip({ children, active }: { children: React.ReactNode; active?: boolean }) {
  return (
    <span
      className={`px-2.5 py-1 rounded-md ${
        active ? "bg-ink-700 text-fg" : "text-fg-muted hover:text-fg cursor-pointer"
      }`}
    >
      {children}
    </span>
  );
}
