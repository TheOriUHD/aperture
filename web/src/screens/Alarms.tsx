import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Bell, Footprints } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { EmptyState } from "../components/ui/EmptyState";
import { Spinner } from "../components/ui/Spinner";
import { api } from "../lib/api";
import { useCameras } from "../hooks/useCameras";
import type { MotionEvent } from "../lib/types";

const DAY_MS = 24 * 60 * 60 * 1000;

const fmtWhen = (ms: number) =>
  new Date(ms).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });

export function Alarms() {
  const navigate = useNavigate();
  const { data: cameras } = useCameras();
  const camName = useMemo(() => {
    const m = new Map<string, string>();
    cameras?.forEach((c) => m.set(c.id, c.name));
    return m;
  }, [cameras]);

  const now = Date.now();
  const { data: events, isLoading } = useQuery({
    queryKey: ["events-all"],
    queryFn: () => api.get<MotionEvent[]>(`/api/events?from=${now - 7 * DAY_MS}&to=${now}&limit=300`),
    refetchInterval: 20000,
  });

  return (
    <>
      <TopBar title="Alarms" />
      <div className="flex-1 overflow-auto p-4">
        {isLoading ? (
          <div className="h-full grid place-items-center">
            <Spinner label="Loading events" />
          </div>
        ) : !events || events.length === 0 ? (
          <EmptyState
            icon={Bell}
            title="No alarms yet"
            hint="Motion events from the last 7 days will appear here."
          />
        ) : (
          <div className="max-w-6xl mx-auto grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-3">
            {events.map((e) => (
              <button
                key={e.id}
                onClick={() => navigate(`/playback?camera=${e.cameraId}&t=${e.startTs}`)}
                className="text-left card overflow-hidden hover:border-line-strong transition-colors"
              >
                <div className="relative aspect-video bg-ink-800">
                  {e.thumbUrl ? (
                    <img src={e.thumbUrl} alt="" className="h-full w-full object-cover" />
                  ) : (
                    <div className="h-full w-full grid place-items-center text-fg-faint">
                      <Footprints className="h-6 w-6" />
                    </div>
                  )}
                  <span className="absolute top-2 left-2 pill-motion">
                    <Footprints className="h-3 w-3" />
                    Motion
                  </span>
                </div>
                <div className="px-3 py-2">
                  <p className="text-sm font-medium text-fg truncate">
                    {camName.get(e.cameraId) ?? "Camera"}
                  </p>
                  <p className="text-xs text-fg-faint">{fmtWhen(e.startTs)}</p>
                </div>
              </button>
            ))}
          </div>
        )}
      </div>
    </>
  );
}
