import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Bell, Footprints, CloudRain, Moon, Sun, Wind, Droplets } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { EmptyState } from "../components/ui/EmptyState";
import { Spinner } from "../components/ui/Spinner";
import { api } from "../lib/api";
import { useCameras } from "../hooks/useCameras";
import { useSite } from "../hooks/useEnvironment";
import { wmoIcon, formatTemp, defaultUnits } from "../lib/weather";
import { fmtDayTime as fmtWhen } from "../lib/format";
import type { MotionEvent } from "../lib/types";

const DAY_MS = 24 * 60 * 60 * 1000;
const WINDY_MS = 8; // ~ fresh breeze

type Filter = "all" | "rain" | "night" | "day" | "windy";

const FILTERS: { key: Filter; label: string; icon: LucideIcon }[] = [
  { key: "all", label: "All", icon: Bell },
  { key: "rain", label: "Rain", icon: CloudRain },
  { key: "night", label: "Night", icon: Moon },
  { key: "day", label: "Day", icon: Sun },
  { key: "windy", label: "Windy", icon: Wind },
];

function matches(e: MotionEvent, f: Filter): boolean {
  const c = e.conditions;
  switch (f) {
    case "rain":
      return (c?.precipMm ?? 0) > 0;
    case "night":
      return c?.isNight === true;
    case "day":
      return c?.isNight === false;
    case "windy":
      return (c?.windSpeedMs ?? 0) >= WINDY_MS;
    default:
      return true;
  }
}

export function Alarms() {
  const navigate = useNavigate();
  const { data: cameras } = useCameras();
  const { data: site } = useSite();
  const units = site?.units ?? defaultUnits;
  const [filter, setFilter] = useState<Filter>("all");

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

  const hasConditions = (events ?? []).some((e) => e.conditions);
  const shown = (events ?? []).filter((e) => matches(e, filter));

  return (
    <>
      <TopBar title="Alarms" />
      <div className="flex-1 overflow-auto p-4">
        {/* Condition filter chips (only when the context layer has data) */}
        {hasConditions && events && events.length > 0 && (
          <div className="max-w-6xl mx-auto mb-3 flex flex-wrap gap-1.5">
            {FILTERS.map(({ key, label, icon: Icon }) => {
              const count = key === "all" ? events.length : events.filter((e) => matches(e, key)).length;
              const active = filter === key;
              return (
                <button
                  key={key}
                  onClick={() => setFilter(key)}
                  className={`flex items-center gap-1.5 rounded-full px-3 py-1 text-xs transition-colors ${
                    active ? "bg-brand text-white" : "bg-ink-700 text-fg-muted hover:bg-ink-650 hover:text-fg"
                  }`}
                >
                  <Icon className="h-3.5 w-3.5" />
                  {label}
                  <span className={active ? "text-white/70" : "text-fg-faint"}>{count}</span>
                </button>
              );
            })}
          </div>
        )}

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
        ) : shown.length === 0 ? (
          <EmptyState icon={Bell} title="No matching events" hint="No events match this filter in the last 7 days." />
        ) : (
          <div className="max-w-6xl mx-auto grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-3">
            {shown.map((e) => {
              const c = e.conditions;
              const CondIcon = wmoIcon(c?.conditionCode, !c?.isNight);
              return (
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
                    {c && (
                      <span className="absolute top-2 right-2 flex items-center gap-1 rounded-md bg-black/45 px-1.5 py-0.5 backdrop-blur-sm">
                        <CondIcon className="h-3 w-3 text-white/90" />
                        {c.tempC != null && (
                          <span className="text-[11px] text-white/90 tabular-nums">{formatTemp(c.tempC, units)}</span>
                        )}
                        {(c.precipMm ?? 0) > 0 && <Droplets className="h-3 w-3 text-[#8ab4f8]" />}
                      </span>
                    )}
                  </div>
                  <div className="px-3 py-2">
                    <p className="text-sm font-medium text-fg truncate">{camName.get(e.cameraId) ?? "Camera"}</p>
                    <p className="text-xs text-fg-faint">{fmtWhen(e.startTs)}</p>
                  </div>
                </button>
              );
            })}
          </div>
        )}
      </div>
    </>
  );
}
