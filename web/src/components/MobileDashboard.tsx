import { useEffect, useMemo, useState, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronDown,
  Check,
  Layers,
  Rows3,
  Grid2x2,
  Grid3x3,
  Footprints,
  PersonStanding,
  Car,
  Package,
  PawPrint,
  Waves,
} from "lucide-react";
import { LiveVideo } from "./LiveVideo";
import { LiveObjectsOverlay } from "./LiveObjectsOverlay";
import { ParkedBadge } from "./ObjectBox";
import { EmptyState } from "./ui/EmptyState";
import { useCameraGroups } from "../hooks/useGroups";
import { api } from "../lib/api";
import { fmtRelative } from "../lib/format";
import type { Camera, MotionEvent } from "../lib/types";

type View = "list" | "grid" | "compact";
const VEHICLE = ["car", "truck", "bus", "motor", "bike", "bicycle", "vehicle"];
const ANIMAL = ["dog", "cat", "bird", "animal", "pet", "horse"];

const startOfToday = () => {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  return d.getTime();
};
const fmtHM = (ms: number) =>
  new Date(ms).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false });

function labelsOf(e: MotionEvent) {
  return [e.topLabel, ...(e.objects?.map((o) => o.label) ?? [])].filter(Boolean).map((l) => l!.toLowerCase());
}

// ---- Detection summary (adaptive) ----------------------------------------

function DetectionSummary({ events }: { events: MotionEvent[] }) {
  const navigate = useNavigate();
  const counts = useMemo(() => {
    let motion = 0,
      person = 0,
      vehicle = 0,
      animal = 0,
      pkg = 0,
      audio = 0;
    for (const e of events) {
      if (e.type === "audio") {
        audio++;
        continue;
      }
      if (e.type === "package") {
        pkg++;
        continue;
      }
      motion++;
      const ls = labelsOf(e);
      if (ls.some((l) => l.includes("person") || l.includes("people"))) person++;
      if (ls.some((l) => VEHICLE.some((v) => l.includes(v)))) vehicle++;
      if (ls.some((l) => ANIMAL.some((a) => l.includes(a)))) animal++;
    }
    return { motion, person, vehicle, animal, pkg, audio };
  }, [events]);

  const chips = [
    { icon: Footprints, label: "Motion", n: counts.motion, always: true },
    { icon: PersonStanding, label: "Person", n: counts.person },
    { icon: Car, label: "Vehicle", n: counts.vehicle },
    { icon: PawPrint, label: "Animal", n: counts.animal },
    { icon: Package, label: "Package", n: counts.pkg },
    { icon: Waves, label: "Audio", n: counts.audio },
  ].filter((c) => c.always || c.n > 0);

  return (
    <button
      onClick={() => navigate("/alarms")}
      className="flex w-full items-center gap-5 overflow-x-auto px-4 py-3 text-left active:bg-white/[0.03]"
    >
      {chips.map((c) => (
        <span key={c.label} className="flex shrink-0 items-center gap-1.5 whitespace-nowrap text-[13px] text-fg-muted">
          <c.icon className="h-4 w-4 text-fg-faint" />
          {c.label}
          <span className="font-semibold tabular-nums text-fg">{c.n}</span>
        </span>
      ))}
      <ChevronDown className="ml-auto h-4 w-4 shrink-0 text-fg-faint" />
    </button>
  );
}

// ---- Recent-detections carousel ------------------------------------------

function DetectionCarousel({ events }: { events: MotionEvent[] }) {
  const navigate = useNavigate();
  const list = events.filter((e) => e.thumbUrl).slice(0, 15);
  if (!list.length) return null;
  return (
    <div className="flex gap-2.5 overflow-x-auto px-4 pb-3 pt-0.5">
      {list.map((e) => (
        <button
          key={e.id}
          onClick={() => navigate(`/playback?camera=${e.cameraId}&t=${e.startTs}`)}
          className="group shrink-0"
        >
          <div className="h-[68px] w-[104px] overflow-hidden rounded-md bg-ink-800 ring-1 ring-inset ring-white/[0.04]">
            <img
              src={e.thumbUrl}
              alt=""
              loading="lazy"
              className="h-full w-full object-cover group-active:brightness-90"
            />
          </div>
          <div className="mt-1.5 text-center text-xs tabular-nums text-fg-muted">{fmtHM(e.startTs)}</div>
        </button>
      ))}
    </div>
  );
}

// ---- Controls: group selector (left) + view mode (right) -----------------

function GroupMenu({
  groups,
  value,
  onChange,
}: {
  groups: { id: string; name: string }[];
  value: string;
  onChange: (id: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const all = [{ id: "all", name: "All Cameras" }, ...groups];
  const cur = all.find((g) => g.id === value) ?? all[0];
  return (
    <div className="relative">
      <button
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className={`flex h-9 items-center gap-2 rounded-md border px-3 text-[13px] font-medium transition-colors ${
          open ? "border-brand/50 bg-brand/10 text-brand" : "border-line-strong bg-ink-800 text-fg"
        }`}
      >
        <Layers className="h-4 w-4 text-brand" />
        <span className="max-w-[9rem] truncate">{cur.name}</span>
        <ChevronDown className={`h-4 w-4 text-fg-faint transition-transform ${open ? "rotate-180" : ""}`} />
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          <div className="absolute left-0 top-11 z-50 max-h-72 w-60 animate-pop-in overflow-auto rounded-lg border border-line bg-ink-800 py-1 shadow-pop">
            {all.map((g) => (
              <button
                key={g.id}
                onClick={() => {
                  onChange(g.id);
                  setOpen(false);
                }}
                className={`flex h-10 w-full items-center gap-2.5 px-3 text-sm ${
                  g.id === value ? "text-brand" : "text-fg/90 hover:bg-white/[0.04] active:bg-white/[0.06]"
                }`}
              >
                <span className="min-w-0 flex-1 truncate text-left">{g.name}</span>
                {g.id === value && <Check className="h-4 w-4 shrink-0" />}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

function ViewMenu({ value, onChange }: { value: View; onChange: (v: View) => void }) {
  const [open, setOpen] = useState(false);
  const opts: { id: View; label: string; icon: typeof Rows3 }[] = [
    { id: "list", label: "List View", icon: Rows3 },
    { id: "grid", label: "Grid View", icon: Grid2x2 },
    { id: "compact", label: "Compact Grid", icon: Grid3x3 },
  ];
  const Cur = opts.find((o) => o.id === value)?.icon ?? Rows3;
  return (
    <div className="relative">
      <button
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className={`grid h-9 w-9 place-items-center rounded-md border text-brand transition-colors ${
          open ? "border-brand/50 bg-brand/10" : "border-line-strong bg-ink-800"
        }`}
        aria-label="View mode"
      >
        <Cur className="h-[18px] w-[18px]" />
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          <div className="absolute right-0 top-11 z-50 w-52 animate-pop-in rounded-lg border border-line bg-ink-800 py-1 shadow-pop">
            {opts.map((o) => (
              <button
                key={o.id}
                onClick={() => {
                  onChange(o.id);
                  setOpen(false);
                }}
                className={`flex h-10 w-full items-center gap-2.5 px-3 text-sm ${
                  o.id === value ? "text-brand" : "text-fg/90 hover:bg-white/[0.04] active:bg-white/[0.06]"
                }`}
              >
                <o.icon className={`h-4 w-4 ${o.id === value ? "text-brand" : "text-fg-faint"}`} />
                <span className="flex-1 text-left">{o.label}</span>
                {o.id === value && <Check className="h-4 w-4" />}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

// ---- Camera feed tile -----------------------------------------------------

function MobileCameraTile({
  camera,
  view,
  lastMs,
  onOpen,
}: {
  camera: Camera;
  view: View;
  lastMs?: number;
  onOpen: (c: Camera) => void;
}) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    if (view !== "list") return;
    const i = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(i);
  }, [view]);
  const pad = (n: number) => String(n).padStart(2, "0");
  const stamp = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ${pad(now.getHours())}:${pad(
    now.getMinutes(),
  )}:${pad(now.getSeconds())}`;

  return (
    <button
      onClick={() => onOpen(camera)}
      className={`group relative block aspect-video w-full overflow-hidden bg-ink-950 ${
        view === "list" ? "" : "rounded-md"
      }`}
    >
      <LiveVideo camera={camera} fit="cover" quality={view === "list" ? "auto" : "low"} videoRef={videoRef} />
      <LiveObjectsOverlay cameraId={camera.id} videoRef={videoRef} fit="cover" />
      <ParkedBadge cameraId={camera.id} className="absolute right-2 top-2 z-[7]" />

      {view === "list" && (
        <div className="pill osd-surface absolute left-2 top-2 tabular-nums text-white/90">
          {stamp} | {camera.name}
        </div>
      )}

      <div
        className={`absolute inset-x-0 bottom-0 flex items-end justify-between gap-2 ${
          view === "list" ? "p-2.5" : view === "grid" ? "p-1.5" : "p-1"
        }`}
      >
        <span
          className={`osd-chip min-w-0 max-w-full ${
            view === "list" ? "h-7 px-2.5" : view === "grid" ? "h-6 px-2 text-xs" : "h-5 rounded px-1.5 text-[10px]"
          }`}
        >
          <span className="truncate">{camera.name}</span>
        </span>
        {view === "list" && lastMs && (
          <span className="pill osd-surface shrink-0 text-white/90">
            <Footprints className="h-3 w-3" />
            {fmtRelative(lastMs)}
          </span>
        )}
      </div>
    </button>
  );
}

// ---- The mobile dashboard -------------------------------------------------

export function MobileDashboard({ cameras, onOpen }: { cameras: Camera[]; onOpen: (c: Camera) => void }) {
  const { data: groups } = useCameraGroups();
  // Poll dropped: event.*/detection.* feed messages invalidate ["events-today"].
  const { data: events } = useQuery({
    queryKey: ["events-today"],
    queryFn: () => api.get<MotionEvent[]>(`/api/events?from=${startOfToday()}&to=${Date.now()}&limit=1000`),
  });
  const evs = events ?? [];

  const [view, setViewState] = useState<View>(() => (localStorage.getItem("ap.mobileView") as View) || "list");
  const [groupId, setGroupState] = useState<string>(() => localStorage.getItem("ap.mobileGroup") || "all");
  const setView = (v: View) => {
    setViewState(v);
    localStorage.setItem("ap.mobileView", v);
  };
  const setGroup = (id: string) => {
    setGroupState(id);
    localStorage.setItem("ap.mobileGroup", id);
  };

  // Resolve the selected group's cameras (fall back to All if it's gone).
  const group = groupId !== "all" ? groups?.find((g) => g.id === groupId) : undefined;
  const effectiveGroup = groupId !== "all" && !group ? "all" : groupId;
  const shown = effectiveGroup === "all" ? cameras : cameras.filter((c) => group?.cameraIds.includes(c.id));

  // Last detection time per camera (from today's events).
  const lastByCam = useMemo(() => {
    const m: Record<string, number> = {};
    for (const e of evs) if (!m[e.cameraId] || e.startTs > m[e.cameraId]) m[e.cameraId] = e.startTs;
    return m;
  }, [evs]);

  const gridClass =
    view === "list" ? "flex flex-col" : view === "grid" ? "grid grid-cols-2 gap-2 p-2" : "grid grid-cols-3 gap-1.5 p-2";

  return (
    <div className="flex-1 overflow-auto">
      <div className="border-b border-line">
        <DetectionSummary events={evs} />
        <DetectionCarousel events={evs} />
        <div className="flex items-center justify-between px-4 py-2.5">
          <GroupMenu groups={groups ?? []} value={effectiveGroup} onChange={setGroup} />
          <ViewMenu value={view} onChange={setView} />
        </div>
      </div>

      {shown.length === 0 ? (
        <EmptyState icon={Layers} title="No cameras in this group" hint="Choose another group above." />
      ) : (
        <div className={gridClass}>
          {shown.map((c) => (
            <MobileCameraTile key={c.id} camera={c} view={view} lastMs={lastByCam[c.id]} onOpen={onOpen} />
          ))}
        </div>
      )}
    </div>
  );
}
