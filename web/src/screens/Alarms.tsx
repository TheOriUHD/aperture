import { Fragment, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  AlertTriangle,
  Bell,
  Car,
  CloudRain,
  Droplets,
  Footprints,
  Moon,
  Package,
  PawPrint,
  PersonStanding,
  Play,
  SearchX,
  Sun,
  Waves,
  Wind,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import clsx from "clsx";
import { TopBar } from "../components/TopBar";
import { EmptyState } from "../components/ui/EmptyState";
import { Skeleton } from "../components/ui/Skeleton";
import { CheckRow, Segmented, SidebarPage, SidebarSearch, SidebarSection } from "../components/ui/Layout";
import { useMediaQuery } from "../hooks/useMediaQuery";
import { api } from "../lib/api";
import { useCameras } from "../hooks/useCameras";
import { useSite } from "../hooks/useEnvironment";
import { wmoIcon, formatTemp, defaultUnits, type Units } from "../lib/weather";
import { fmtDate, fmtDateTime, fmtDuration, fmtTimeSec } from "../lib/format";
import type { MotionEvent } from "../lib/types";

const HOUR_MS = 60 * 60 * 1000;
const DAY_MS = 24 * HOUR_MS;
const WINDY_MS = 8; // ~ fresh breeze

type Filter = "all" | "rain" | "night" | "day" | "windy";

const FILTERS: { key: Filter; label: string; icon: LucideIcon }[] = [
  { key: "all", label: "All", icon: Bell },
  { key: "rain", label: "Rain", icon: CloudRain },
  { key: "night", label: "Night", icon: Moon },
  { key: "day", label: "Day", icon: Sun },
  { key: "windy", label: "Windy", icon: Wind },
];

// Time ranges narrow the 7 days the screen already loads; nothing here asks the
// server for more, so the widest option is the whole fetched window.
type Range = "1h" | "1d" | "3d" | "1w";
const RANGES: { value: Range; label: string; title: string; ms: number }[] = [
  { value: "1h", label: "1h", title: "Last hour", ms: HOUR_MS },
  { value: "1d", label: "1D", title: "Last 24 hours", ms: DAY_MS },
  { value: "3d", label: "3D", title: "Last 3 days", ms: 3 * DAY_MS },
  { value: "1w", label: "1W", title: "Last 7 days", ms: 7 * DAY_MS },
];

const FETCH_LIMIT = 300;
const VEHICLE = ["car", "truck", "bus", "motor", "bike", "bicycle", "vehicle"];
const ANIMAL = ["dog", "cat", "bird", "animal", "pet", "horse"];

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

function labelsOf(e: MotionEvent): string[] {
  return [e.topLabel, ...(e.objects?.map((o) => o.label) ?? [])].filter(Boolean).map((l) => l!.toLowerCase());
}

// The icon for an event's type pill: the detected object when there is one,
// otherwise plain motion (or a sound, for audio events).
function typeIcon(e: MotionEvent): LucideIcon {
  if (e.type === "audio") return Waves;
  if (e.type === "package") return Package;
  const ls = labelsOf(e);
  if (ls.some((l) => l.includes("person") || l.includes("people"))) return PersonStanding;
  if (ls.some((l) => VEHICLE.some((v) => l.includes(v)))) return Car;
  if (ls.some((l) => ANIMAL.some((a) => l.includes(a)))) return PawPrint;
  return Footprints;
}

function typeLabel(e: MotionEvent): string {
  return e.objectSummary || (e.type === "audio" ? "Audio" : "Motion");
}

function dayKey(ms: number): string {
  return new Date(ms).toDateString();
}

function dayLabel(ms: number): string {
  const d = new Date(ms);
  const today = new Date();
  const yesterday = new Date();
  yesterday.setDate(today.getDate() - 1);
  if (d.toDateString() === today.toDateString()) return "Today";
  if (d.toDateString() === yesterday.toDateString()) return "Yesterday";
  return `${d.toLocaleDateString(undefined, { weekday: "long" })}, ${fmtDate(ms)}`;
}

export function Alarms() {
  const navigate = useNavigate();
  const desktop = useMediaQuery("(min-width: 1024px)");
  const { data: cameras } = useCameras();
  const { data: site } = useSite();
  const units = site?.units ?? defaultUnits;
  const [filter, setFilter] = useState<Filter>("all");
  const [range, setRange] = useState<Range>("1w");
  const [search, setSearch] = useState("");
  const [camSel, setCamSel] = useState<Set<string>>(() => new Set());

  const camName = useMemo(() => {
    const m = new Map<string, string>();
    cameras?.forEach((c) => m.set(c.id, c.name));
    return m;
  }, [cameras]);

  const now = Date.now();
  // Poll dropped: event.*/detection.* feed messages invalidate ["events-all"].
  const {
    data: events,
    isLoading,
    isError,
    refetch,
  } = useQuery({
    queryKey: ["events-all"],
    queryFn: () => api.get<MotionEvent[]>(`/api/events?from=${now - 7 * DAY_MS}&to=${now}&limit=${FETCH_LIMIT}`),
  });

  const all = useMemo(() => events ?? [], [events]);
  const hasConditions = all.some((e) => e.conditions);

  // Every filter runs client-side over the fetched events. Counts are faceted:
  // each section counts what it would show with the *other* filters applied.
  const cutoff = now - (RANGES.find((r) => r.value === range)?.ms ?? 7 * DAY_MS);
  const q = search.trim().toLowerCase();
  const inRange = (e: MotionEvent) => e.startTs >= cutoff;
  const camOK = (e: MotionEvent) => camSel.size === 0 || camSel.has(e.cameraId);
  const searchOK = (e: MotionEvent) =>
    !q ||
    `${camName.get(e.cameraId) ?? ""} ${typeLabel(e)} ${e.type} ${labelsOf(e).join(" ")}`.toLowerCase().includes(q);

  const shown = all.filter((e) => inRange(e) && matches(e, filter) && camOK(e) && searchOK(e));

  const condBase = all.filter((e) => inRange(e) && camOK(e) && searchOK(e));
  const condCount = (f: Filter) => condBase.filter((e) => matches(e, f)).length;

  // Cameras present in the fetched events, by name, with how many events each
  // contributes under the current time/condition/search filters.
  const camCounts = new Map<string, number>();
  for (const e of all) {
    const hit = inRange(e) && matches(e, filter) && searchOK(e) ? 1 : 0;
    camCounts.set(e.cameraId, (camCounts.get(e.cameraId) ?? 0) + hit);
  }
  const camRows = [...camCounts.entries()]
    .map(([id, n]) => ({ id, name: camName.get(id) ?? "Camera", n }))
    .sort((a, b) => a.name.localeCompare(b.name));

  const showConditions = hasConditions && all.length > 0;
  const filtered = filter !== "all" || range !== "1w" || !!q || camSel.size > 0;
  const clearFilters = () => {
    setFilter("all");
    setRange("1w");
    setSearch("");
    setCamSel(new Set());
  };
  const toggleCam = (id: string, on: boolean) =>
    setCamSel((s) => {
      const n = new Set(s);
      if (on) n.add(id);
      else n.delete(id);
      return n;
    });

  const sidebar = (
    <div>
      <SidebarSearch value={search} onChange={setSearch} placeholder="Search" />
      <div className="pt-3">
        <Segmented
          value={range}
          onChange={setRange}
          full
          options={RANGES.map((r) => ({ value: r.value, label: r.label, title: r.title }))}
        />
      </div>

      {(showConditions || camRows.length > 0) && (
        <div className="mt-3 border-t border-line">
          {/* Condition filters (only when the context layer has data) */}
          {showConditions && (
            <SidebarSection title="Conditions" defaultOpen={desktop}>
              <div role="radiogroup" aria-label="Conditions">
                {FILTERS.map(({ key, label, icon: Icon }) => (
                  <label
                    key={key}
                    className={clsx(
                      "flex h-8 cursor-pointer items-center gap-2.5 rounded-md px-1 text-sm hover:bg-white/[0.03]",
                      filter === key ? "text-fg" : "text-fg/90",
                    )}
                  >
                    <input
                      type="radio"
                      name="alarm-condition"
                      checked={filter === key}
                      onChange={() => setFilter(key)}
                    />
                    <Icon className={clsx("h-4 w-4 shrink-0", filter === key ? "text-brand" : "text-fg-muted")} />
                    <span className="min-w-0 flex-1 truncate">{label}</span>
                    <span className="tabular-nums text-xs text-fg-faint">{condCount(key)}</span>
                  </label>
                ))}
              </div>
            </SidebarSection>
          )}

          {camRows.length > 0 && (
            <SidebarSection
              title="Cameras"
              defaultOpen={desktop}
              action={
                camSel.size > 0 ? (
                  <button className="btn-link text-xs" onClick={() => setCamSel(new Set())}>
                    Reset
                  </button>
                ) : undefined
              }
            >
              {camRows.map((c) => (
                <CheckRow
                  key={c.id}
                  label={c.name}
                  count={c.n}
                  checked={camSel.has(c.id)}
                  onChange={(v) => toggleCam(c.id, v)}
                />
              ))}
            </SidebarSection>
          )}
        </div>
      )}

      {filtered && (
        <button className="btn-link mt-3 px-1" onClick={clearFilters}>
          Clear Filters
        </button>
      )}
    </div>
  );

  // Group consecutive events by calendar day (the API returns newest first).
  const groups: { key: string; label: string; items: MotionEvent[] }[] = [];
  for (const e of shown) {
    const k = dayKey(e.startTs);
    const last = groups[groups.length - 1];
    if (last && last.key === k) last.items.push(e);
    else groups.push({ key: k, label: dayLabel(e.startTs), items: [e] });
  }

  return (
    <>
      <TopBar title="Alarms" />
      <SidebarPage sidebar={sidebar} storageKey="alarms">
        <div className="min-h-0 flex-1 overflow-auto px-4 pb-6 lg:px-5">
          {isLoading ? (
            <div aria-busy="true" aria-label="Loading events">
              <div className="flex h-12 items-center">
                <Skeleton className="h-4 w-24" />
              </div>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-[repeat(auto-fill,minmax(220px,1fr))] sm:gap-3">
                {Array.from({ length: 15 }, (_, i) => (
                  <div key={i} className="overflow-hidden rounded-lg border border-line bg-ink-800">
                    <Skeleton className="aspect-video w-full !rounded-none" />
                    <div className="space-y-2 px-3 py-2.5">
                      <Skeleton className="h-3.5 w-2/3" />
                      <Skeleton className="h-3 w-1/3" />
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ) : isError && !events ? (
            <EmptyState
              icon={AlertTriangle}
              title="Couldn't load alarms"
              hint="The event list didn't load. Check the connection to the NVR and try again."
              action={
                <button className="btn-ghost" onClick={() => void refetch()}>
                  Try again
                </button>
              }
            />
          ) : all.length === 0 ? (
            <EmptyState icon={Bell} title="No alarms yet" hint="Motion events from the last 7 days will appear here." />
          ) : shown.length === 0 ? (
            <EmptyState
              icon={SearchX}
              title="No matching events"
              hint="No events match these filters in the last 7 days."
              action={
                <button className="btn-link" onClick={clearFilters}>
                  Clear Filters
                </button>
              }
            />
          ) : (
            <>
              {groups.map((g) => (
                <Fragment key={g.key}>
                  <div className="sticky top-0 z-[5] -mx-4 flex h-12 items-center gap-2 bg-ink-900 px-4 lg:-mx-5 lg:px-5">
                    <h2 className="text-[13px] font-semibold text-fg">{g.label}</h2>
                    <span className="text-xs tabular-nums text-fg-faint">{g.items.length}</span>
                  </div>
                  <div className="grid grid-cols-2 gap-2 sm:grid-cols-[repeat(auto-fill,minmax(220px,1fr))] sm:gap-3 pb-3">
                    {g.items.map((e) => (
                      <EventCard
                        key={e.id}
                        e={e}
                        camera={camName.get(e.cameraId) ?? "Camera"}
                        units={units}
                        onOpen={() => navigate(`/playback?camera=${e.cameraId}&t=${e.startTs}`)}
                      />
                    ))}
                  </div>
                </Fragment>
              ))}
              <p className="pt-4 text-center text-sm text-fg-faint">
                {shown.length === all.length
                  ? all.length >= FETCH_LIMIT
                    ? `Showing the latest ${all.length} events from the last 7 days`
                    : `Showing all ${all.length} events`
                  : `Showing ${shown.length} of ${all.length} events`}
              </p>
            </>
          )}
        </div>
      </SidebarPage>
    </>
  );
}

// ---- Event card ----------------------------------------------------------------

function EventCard({ e, camera, units, onOpen }: { e: MotionEvent; camera: string; units: Units; onOpen: () => void }) {
  const c = e.conditions;
  const CondIcon = wmoIcon(c?.conditionCode, !c?.isNight);
  const TypeIcon = typeIcon(e);
  const smart = !!e.objectSummary || !!e.topLabel || (e.objects?.length ?? 0) > 0;
  const end = e.endTsMs ?? e.endTs;
  return (
    <button
      onClick={onOpen}
      title={`${camera} · ${fmtDateTime(e.startTs)}`}
      className={clsx(
        "group flex flex-col overflow-hidden rounded-lg border border-line bg-ink-800 text-left transition-colors",
        "hover:border-line-strong hover:bg-ink-750",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/60",
      )}
    >
      <div className="relative aspect-video w-full overflow-hidden bg-ink-950">
        {e.thumbUrl ? (
          <img
            src={e.thumbUrl}
            alt=""
            loading="lazy"
            decoding="async"
            className="h-full w-full object-cover transition-[filter] duration-150 group-hover:brightness-110"
          />
        ) : (
          <div className="grid h-full w-full place-items-center text-fg-faint">
            <TypeIcon className="h-6 w-6" strokeWidth={1.5} />
          </div>
        )}
        {e.objects?.map((o, i) => (
          <div
            key={i}
            className="pointer-events-none absolute rounded-sm border border-brand/90"
            style={{
              left: `${o.x * 100}%`,
              top: `${o.y * 100}%`,
              width: `${o.w * 100}%`,
              height: `${o.h * 100}%`,
            }}
          >
            <span className="absolute left-0 top-0 -translate-y-full whitespace-nowrap rounded-sm bg-brand-solid px-1 text-[9px] font-medium capitalize text-white">
              {o.label}
            </span>
          </div>
        ))}

        {/* Type pill */}
        <span
          className={clsx(
            "pill absolute left-2 top-2 capitalize",
            smart ? "bg-brand-solid text-white" : "osd-surface text-white/90",
          )}
        >
          <TypeIcon className="h-3 w-3" />
          <span className="max-w-[9rem] truncate">{typeLabel(e)}</span>
        </span>

        {/* Conditions at the time of the event */}
        {c && (
          <span className="pill osd-surface absolute right-2 top-2 text-white/90">
            <CondIcon className="h-3 w-3" />
            {c.tempC != null && <span className="tabular-nums">{formatTemp(c.tempC, units)}</span>}
            {(c.precipMm ?? 0) > 0 && <Droplets className="h-3 w-3 text-motion" />}
          </span>
        )}

        {/* Hover affordance: opens playback at this moment */}
        <span className="osd-btn pointer-events-none absolute bottom-2 right-2 opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-visible:opacity-100">
          <Play className="h-4 w-4" fill="currentColor" />
        </span>
      </div>
      <div className="flex min-w-0 items-end gap-2 px-3 py-2.5">
        <div className="min-w-0 flex-1">
          <p className="truncate text-[13px] font-medium text-fg">{camera}</p>
          <p className="mt-0.5 text-xs tabular-nums text-fg-faint">{fmtTimeSec(e.startTs)}</p>
        </div>
        {e.ongoing ? (
          <span className="flex shrink-0 items-center gap-1 text-xs text-rec">
            <span className="h-1.5 w-1.5 animate-rec-pulse rounded-full bg-rec" />
            Live
          </span>
        ) : end > e.startTs ? (
          <span className="shrink-0 text-xs tabular-nums text-fg-faint">{fmtDuration(end - e.startTs)}</span>
        ) : null}
      </div>
    </button>
  );
}
