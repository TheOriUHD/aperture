import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ExtensionSlot } from "../ext/ExtensionSlot";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronRight,
  CircleCheck,
  Server,
  Activity,
  Info,
  Plus,
  Download,
  PersonStanding,
  Car,
  PawPrint,
  Footprints,
  Package,
  Waves,
} from "lucide-react";
import { useStorage } from "../hooks/useStorage";
import { useSystemStatus, useSiteName, useHASensors, useBitrate, type BitratePoint } from "../hooks/useSystem";
import { useWeatherNow, useSite } from "../hooks/useEnvironment";
import { wmoIcon, formatTemp } from "../lib/weather";
import { formatBytes, fmtDuration } from "../lib/format";
import { ExportCenter } from "./ExportCenter";
import { CapacityBar, TierChips, pctText } from "./CapacityMeter";
import { limitText } from "../lib/capacity";
import { useCapacity } from "../hooks/useHardware";
import { useAuth } from "../lib/auth";
import { api } from "../lib/api";
import type { Camera, MotionEvent } from "../lib/types";


function humanAge(ms: number): string {
  if (!ms) return "—";
  let s = Math.floor((Date.now() - ms) / 1000);
  if (s < 0) s = 0;
  const y = Math.floor(s / 31_536_000);
  s -= y * 31_536_000;
  const mo = Math.floor(s / 2_592_000);
  s -= mo * 2_592_000;
  const d = Math.floor(s / 86_400);
  const h = Math.floor((s % 86_400) / 3600);
  if (y) return `${y}y ${mo}m ${d}d`;
  if (mo) return `${mo}m ${d}d`;
  if (d) return `${d}d ${h}h`;
  const m = Math.floor((s % 3600) / 60);
  return h ? `${h}h ${m}m` : `${m}m`;
}

function humanUptime(sec: number): string {
  const d = Math.floor(sec / 86_400);
  const h = Math.floor((sec % 86_400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  return `${m}m`;
}

function formatBps(bps: number): string {
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)} Gbps`;
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)} Mbps`;
  if (bps >= 1e3) return `${Math.round(bps / 1e3)} kbps`;
  return `${Math.round(bps)} bps`;
}

// UniFi-style detection timestamp: "14 Minutes Ago", "Today, 01:15", etc.
function detTime(ms: number): string {
  const d = new Date(ms);
  const now = new Date();
  const mins = Math.floor((Date.now() - ms) / 60_000);
  if (mins < 60) {
    const m = Math.max(1, mins);
    return `${m} Minute${m === 1 ? "" : "s"} Ago`;
  }
  const hm = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false });
  if (d.toDateString() === now.toDateString()) return `Today, ${hm}`;
  const y = new Date(now);
  y.setDate(now.getDate() - 1);
  if (d.toDateString() === y.toDateString()) return `Yesterday, ${hm}`;
  return `${d.toLocaleDateString(undefined, { month: "short", day: "numeric" })}, ${hm}`;
}

function detIcon(e: MotionEvent) {
  const labels = [e.topLabel, ...(e.objects?.map((o) => o.label) ?? [])].filter(Boolean).map((l) => l!.toLowerCase());
  if (e.type === "package") return Package;
  if (labels.some((l) => l.includes("person") || l.includes("people"))) return PersonStanding;
  if (labels.some((l) => ["car", "truck", "bus", "vehicle", "motor", "bike", "bicycle"].some((v) => l.includes(v))))
    return Car;
  if (labels.some((l) => ["dog", "cat", "animal", "bird", "pet", "horse"].some((a) => l.includes(a)))) return PawPrint;
  if (e.type === "audio") return Waves;
  return Footprints;
}

// ---- Environment header (weather · temperature · clock) ------------------

function EnvironmentBody() {
  const { data: weather } = useWeatherNow();
  const { data: site } = useSite();
  const { data: settings } = useSiteName();
  const sensorId = settings?.overviewTempSensor || "";
  const { data: sensors } = useHASensors(!!sensorId);

  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const i = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(i);
  }, []);

  const sample = weather?.sample;
  const hour = now.getHours();
  const Icon = wmoIcon(sample?.conditionCode, hour >= 6 && hour < 20);
  const sensor = sensorId ? sensors?.find((s) => s.entityId === sensorId) : undefined;
  const tempStr = sensor ? `${sensor.value}${sensor.unit || "°"}` : formatTemp(sample?.tempC, site?.units);
  const clock = now.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });

  return (
    <div className="flex items-center justify-center gap-2.5 text-fg">
      <Icon className="h-5 w-5 shrink-0 text-fg-muted" />
      <span className="text-[15px] font-medium">{tempStr}</span>
      <span className="text-fg-faint">/</span>
      <span className="text-[15px] font-medium tabular-nums">{clock}</span>
    </div>
  );
}

// ---- System & storage ----------------------------------------------------

function SystemBody({ cameras }: { cameras: Camera[] }) {
  const navigate = useNavigate();
  const { data: storage } = useStorage();
  const { data: status } = useSystemStatus();
  const cap = useCapacity();
  const { user } = useAuth();

  const online = cameras.filter((c) => c.enabled).length;
  const oldestList = storage?.cameras.filter((c) => c.oldestTs > 0).map((c) => c.oldestTs) ?? [];
  const oldest = oldestList.length ? Math.min(...oldestList) : 0;
  const used = storage?.usedBytes ?? 0;
  // Total allocation across ALL destinations; `allocatedBytes` is local-only and
  // would understate capacity whenever a network location exists.
  const alloc = storage?.totalAllocatedBytes ?? storage?.allocatedBytes ?? 0;
  const total = alloc > 0 ? alloc : storage?.diskTotalBytes ?? 0;

  return (
    <>
      <button
        onClick={() => navigate("/settings?tab=storage")}
        className="group -mx-1 flex w-[calc(100%+0.5rem)] items-center gap-2.5 rounded-lg px-1 py-1 text-left hover:bg-ink-850"
      >
        <Server className="h-6 w-6 shrink-0 text-fg-muted" strokeWidth={1.5} />
        <CircleCheck className="h-5 w-5 shrink-0 text-online" />
        <span className="flex items-baseline gap-1.5">
          <span className="text-base font-medium text-fg">{formatBytes(used)}</span>
          <span className="text-xs text-fg-faint">/ {formatBytes(total)}</span>
        </span>
        <ChevronRight className="ml-auto h-5 w-5 text-fg-faint group-hover:text-fg" />
      </button>

      <dl className="mt-3.5 space-y-3 text-sm">
        <Row label="Oldest recording" value={oldest > 0 ? humanAge(oldest) : "—"} />
        <Row label={`Aperture ${status?.version ?? ""}`.trim()} value={status ? "Running" : "…"} ok />
        <Row label={`Cameras (${cameras.length})`} value={`${online} online`} ok={online === cameras.length} />
        <Row label="Uptime" value={status ? humanUptime(status.uptimeSec) : "…"} />
      </dl>

      <CapacityRow cameras={cameras.length} state={cap} canSetUp={user?.role === "admin"} onSetUp={() => navigate("/settings?tab=hardware")} />
    </>
  );
}

function Row({ label, value, ok }: { label: string; value: string; ok?: boolean }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <dt className="truncate text-fg-muted">{label}</dt>
      <dd className={`shrink-0 tabular-nums ${ok ? "text-online" : "text-fg"}`}>{value}</dd>
    </div>
  );
}

// Camera Capacity: how much of this machine the cameras use, from the hardware
// profile (Settings › Hardware). Without one there is nothing honest to show, so
// it asks for it instead of guessing.
function CapacityRow({
  cameras,
  state,
  canSetUp,
  onSetUp,
}: {
  cameras: number;
  state: ReturnType<typeof useCapacity>;
  canSetUp: boolean;
  onSetUp: () => void;
}) {
  const c = state.capacity;
  const tone = !c ? "bg-fg-faint" : c.pct > 1 ? "bg-danger" : c.pct > 0.85 ? "bg-warn" : "bg-online";
  return (
    <div className="mt-4">
      <div className="mb-1.5 flex items-center gap-2 text-sm">
        <span className="text-fg-muted">Camera Capacity</span>
        <span className={`h-1.5 w-1.5 rounded-full ${tone}`} />
        <span className="font-medium text-fg">{cameras}</span>
        {c && (
          <span
            className={`ml-auto text-xs tabular-nums ${c.pct > 1 ? "text-danger" : c.pct > 0.85 ? "text-warn" : "text-fg-faint"}`}
            title={`Limited by ${limitText(c.limitedBy)} · runs about ${c.supports.map((s) => `${s.count} × ${s.label}`).join(", ")}`}
          >
            {c.pct > 1 ? `Over capacity · ${pctText(c.pct)}` : `${pctText(c.pct)} used`}
          </span>
        )}
      </div>
      {c ? (
        <>
          <button onClick={canSetUp ? onSetUp : undefined} className="block w-full" title="Hardware and capacity">
            <CapacityBar capacity={c} />
          </button>
          <div className="mt-3">
            <TierChips capacity={c} />
          </div>
        </>
      ) : state.loading ? (
        <div className="h-2 w-full animate-pulse rounded-full bg-ink-700" />
      ) : (
        <div className="rounded-md border border-dashed border-line-strong px-3 py-2.5 text-xs leading-relaxed text-fg-muted">
          Set the hardware Aperture runs on to see how many cameras it handles.
          {canSetUp && (
            <button className="btn-link mt-1 block text-xs" onClick={onSetUp}>
              Set up hardware
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// ---- Real-time bitrate ---------------------------------------------------

function BitrateChart({ points }: { points: BitratePoint[] }) {
  if (points.length < 2) {
    return <div className="grid h-full place-items-center text-xs text-fg-faint">Gathering data…</div>;
  }
  const max = Math.max(1, ...points.map((p) => p.bps));
  const n = points.length;
  const X = (i: number) => (i / (n - 1)) * 100;
  const Y = (v: number) => 96 - (v / max) * 88;
  const line = points.map((p, i) => `${i === 0 ? "M" : "L"}${X(i).toFixed(2)},${Y(p.bps).toFixed(2)}`).join(" ");
  const area = `M0,100 ${points.map((p, i) => `L${X(i).toFixed(2)},${Y(p.bps).toFixed(2)}`).join(" ")} L100,100 Z`;
  return (
    <svg viewBox="0 0 100 100" preserveAspectRatio="none" className="h-full w-full">
      <defs>
        <linearGradient id="brate" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="rgba(71,151,255,0.32)" />
          <stop offset="100%" stopColor="rgba(71,151,255,0)" />
        </linearGradient>
      </defs>
      <path d={area} fill="url(#brate)" />
      <path d={line} fill="none" stroke="#4797ff" strokeWidth={1.6} vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
    </svg>
  );
}

function BitrateBody() {
  const { data } = useBitrate();
  const points = data?.buckets ?? [];
  return (
    <>
      <div className="mb-3 flex items-center gap-2">
        <Activity className="h-4 w-4 text-fg-muted" />
        <h3 className="text-sm font-medium text-fg">Real-time Bitrate</h3>
      </div>
      <div className="h-24">
        <BitrateChart points={points} />
      </div>
      <div className="mt-3 flex items-center justify-between border-t border-line pt-3">
        <span className="flex items-center gap-1.5 text-sm text-fg-muted">
          Current Bitrate
          <Info className="h-3.5 w-3.5 text-fg-faint" />
        </span>
        <span className="text-base font-medium tabular-nums text-brand">{formatBps(data?.currentBps ?? 0)}</span>
      </div>
    </>
  );
}

// ---- Spotlights ----------------------------------------------------------

// Actions intentionally unwired for now — the card's visibility is controlled by
// a global admin toggle (Settings › General › System).
function SpotlightsCard() {
  return (
    <div className="rounded-lg border border-line p-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-fg">Spotlights</h3>
        <ChevronRight className="h-4 w-4 text-fg-faint" />
      </div>
      <p className="mt-2 text-sm leading-relaxed text-fg-muted">
        Spotlights help you monitor what matters most — people, vehicles, packages, and more — across all your cameras.
      </p>
      <button
        type="button"
        className="mt-3 flex items-center gap-1.5 text-sm font-medium text-brand hover:text-brand-hover"
      >
        <span className="grid h-4 w-4 place-items-center rounded-full border border-current">
          <Plus className="h-2.5 w-2.5" />
        </span>
        Create a Spotlight
      </button>
    </div>
  );
}

// ---- Video archiving -----------------------------------------------------

function VideoArchivingCard({ cameras }: { cameras: Camera[] }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex items-center justify-between rounded-lg border border-line p-4">
      <h3 className="text-sm font-medium text-fg">Video Archiving</h3>
      <button className="btn-ghost h-8" onClick={() => setOpen(true)} disabled={cameras.length === 0}>
        <Download className="h-4 w-4" />
        Download
      </button>
      <ExportCenter open={open} onClose={() => setOpen(false)} camera={cameras[0] ?? null} cameras={cameras} defaultTo={Date.now()} />
    </div>
  );
}

// ---- Recent detections ---------------------------------------------------

// Takes EVERY camera, disabled included: an event recorded before its camera was
// switched off should still be labelled with that camera's name.
function RecentDetectionsCard({ cameras }: { cameras: Camera[] }) {
  const navigate = useNavigate();
  // Poll dropped: event.*/detection.* feed messages invalidate ["events-recent-dash"].
  const { data: events } = useQuery({
    queryKey: ["events-recent-dash"],
    queryFn: () => api.get<MotionEvent[]>(`/api/events?from=${Date.now() - 2 * 864e5}&to=${Date.now()}&limit=20`),
  });
  const nameOf = (id: string) => cameras.find((c) => c.id === id)?.name ?? "Camera";
  const list = (events ?? []).slice(0, 6);

  return (
    <div className="overflow-hidden rounded-lg border border-line">
      <button
        onClick={() => navigate("/alarms")}
        className="flex w-full items-center justify-between p-4 hover:bg-ink-850"
      >
        <h3 className="text-sm font-medium text-fg">Recent Detections</h3>
        <ChevronRight className="h-4 w-4 text-fg-faint" />
      </button>
      <div className="divide-y divide-line border-t border-line">
        {list.length === 0 ? (
          <p className="p-4 text-xs text-fg-faint">No detections yet.</p>
        ) : (
          list.map((e) => {
            const Icon = detIcon(e);
            const dur = (e.endTs || e.startTs) - e.startTs;
            return (
              <button
                key={e.id}
                onClick={() => navigate(`/playback?camera=${e.cameraId}&t=${e.startTs}`)}
                className="flex w-full items-center gap-3 px-4 py-2.5 text-left hover:bg-ink-850"
              >
                <div className="h-11 w-16 shrink-0 overflow-hidden rounded bg-ink-800">
                  {e.thumbUrl ? <img src={e.thumbUrl} alt="" className="h-full w-full object-cover" /> : null}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm text-fg">{detTime(e.startTs)}</div>
                  <div className="mt-0.5 flex items-center gap-1.5 text-xs text-fg-muted">
                    <Icon className="h-3.5 w-3.5 shrink-0" />
                    <span className="truncate">{nameOf(e.cameraId)}</span>
                  </div>
                </div>
                <span className="shrink-0 text-xs tabular-nums text-fg-faint">
                  {dur < 60_000 ? `${Math.max(1, Math.round(dur / 1000))}s` : fmtDuration(dur)}
                </span>
              </button>
            );
          })
        )}
      </div>
    </div>
  );
}

// ---- The sidebar ---------------------------------------------------------

export function OverviewSidebar({
  cameras,
  allCameras,
}: {
  // The cameras actually on screen: counts, resolutions and the export picker
  // describe what the operator can see.
  cameras: Camera[];
  // Every camera, disabled included — used only to name past events.
  allCameras?: Camera[];
}) {
  const { data: settings } = useSiteName();
  const showSpotlights = settings?.spotlightsEnabled !== false; // default on until loaded
  return (
    <div className="space-y-3">
      <div className="overflow-hidden rounded-lg border border-line">
        <div className="px-4 py-3">
          <EnvironmentBody />
        </div>
        <div className="border-t border-line p-4">
          <SystemBody cameras={cameras} />
        </div>
      </div>
      <div className="rounded-lg border border-line p-4">
        <BitrateBody />
      </div>
      {showSpotlights && <SpotlightsCard />}
      <VideoArchivingCard cameras={cameras} />
      <RecentDetectionsCard cameras={allCameras ?? cameras} />
      <ExtensionSlot slot="dashboard-card" />
    </div>
  );
}
