import { Activity } from "lucide-react";

// The event strip mirrors UniFi's Playback footer. Motion events populate it in
// Phase 2; for now it shows the filter shell and an empty state.
export function EventStrip({ dateLabel }: { dateLabel: string }) {
  return (
    <div className="border-t border-line bg-ink-900">
      <div className="flex items-center gap-3 px-4 h-11 border-b border-line">
        <span className="text-xs font-medium text-fg">{dateLabel}</span>
        <div className="w-px h-4 bg-line" />
        <div className="flex items-center gap-1 text-xs">
          <Chip active>All</Chip>
          <Chip>Motion</Chip>
        </div>
        <span className="ml-auto text-xs text-fg-faint">Detections</span>
      </div>
      <div className="h-[108px] grid place-items-center text-center px-4">
        <div className="flex flex-col items-center gap-1.5 text-fg-faint">
          <Activity className="h-5 w-5" />
          <span className="text-xs">Motion events will appear here once detection is enabled</span>
        </div>
      </div>
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
