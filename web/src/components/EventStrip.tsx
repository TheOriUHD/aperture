import { useRef, useState } from "react";
import { Activity, CalendarDays, Footprints, Car, Info, Package, PawPrint, PersonStanding, Sparkles, Waves } from "lucide-react";
import type { MotionEvent } from "../lib/types";
import { fmtRelative } from "../lib/format";

type Mode = "smart" | "basic" | "all";
type TypeFilter = "all" | "person" | "vehicle" | "animal" | "package" | "audio";

const VEHICLE = ["car", "truck", "bus", "motorcycle", "motorbike", "bicycle", "vehicle"];
const ANIMAL = ["dog", "cat", "bird", "animal", "pet", "horse"];

const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));

function labelsOf(e: MotionEvent): string[] {
  const ls = new Set<string>();
  if (e.topLabel) ls.add(e.topLabel.toLowerCase());
  e.objects?.forEach((o) => o.label && ls.add(o.label.toLowerCase()));
  return [...ls];
}
// "Smart" = an object was classified; "Basic" = plain motion with no label.
function isSmart(e: MotionEvent): boolean {
  return !!e.topLabel || (e.objects?.length ?? 0) > 0;
}
function eventType(e: MotionEvent): TypeFilter | "motion" {
  if (e.type === "audio") return "audio";
  if (e.type === "package") return "package";
  const ls = labelsOf(e);
  if (ls.some((l) => l.includes("person") || l.includes("people"))) return "person";
  if (ls.some((l) => VEHICLE.some((v) => l.includes(v)))) return "vehicle";
  if (ls.some((l) => ANIMAL.some((a) => l.includes(a)))) return "animal";
  return "motion";
}

function TypeIcon({ e, className }: { e: MotionEvent; className?: string }) {
  const t = eventType(e);
  const Icon =
    t === "person"
      ? PersonStanding
      : t === "vehicle"
        ? Car
        : t === "animal"
          ? PawPrint
          : t === "package"
            ? Package
            : t === "audio"
              ? Waves
              : Footprints;
  return <Icon className={className} />;
}

// UniFi-style Playback footer: filterable event thumbnails; click to jump the
// player there. Drag the top edge to grow it into a multi-row grid.
export function EventStrip({
  dateLabel,
  events,
  onSelect,
}: {
  dateLabel: string;
  events: MotionEvent[];
  onSelect: (startMs: number) => void;
}) {
  const [mode, setMode] = useState<Mode>("all");
  const [typeF, setTypeF] = useState<TypeFilter>("all");
  const [h, setH] = useState(120);
  const drag = useRef<{ y: number; h: number } | null>(null);

  const filtered = events
    .filter((e) => {
      // Smart/Basic tabs classify motion events by object detection; audio events
      // are neither, so they only appear under "All".
      const audio = e.type === "audio";
      if (mode === "smart") return !audio && isSmart(e);
      if (mode === "basic") return !audio && !isSmart(e);
      return true;
    })
    .filter((e) => {
      if (typeF === "all") return true;
      if (typeF === "audio") return e.type === "audio";
      return e.type !== "audio" && eventType(e) === typeF;
    })
    .sort((a, b) => b.startTs - a.startTs);

  const tall = h > 160;

  function onDown(e: React.PointerEvent) {
    drag.current = { y: e.clientY, h };
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
  }
  function onMove(e: React.PointerEvent) {
    if (!drag.current) return;
    setH(clamp(drag.current.h + (drag.current.y - e.clientY), 96, 460));
  }
  function onUp() {
    drag.current = null;
  }

  return (
    <div className="flex shrink-0 flex-col">
      {/* drag handle — it is also the gap between the player row and this
          frame, so resizing happens exactly where the two meet */}
      <div
        onPointerDown={onDown}
        onPointerMove={onMove}
        onPointerUp={onUp}
        className="group flex h-3 cursor-ns-resize touch-none items-center justify-center"
        title="Drag to resize"
      >
        <div className="h-1 w-10 rounded-full bg-line-strong transition-colors group-hover:bg-fg-faint" />
      </div>

      <div className="frame flex min-w-0 flex-col">
        {/* filter row */}
        <div className="flex flex-wrap items-center gap-2 px-4 py-3">
          <span className="mr-1 flex items-center gap-1.5 text-sm text-fg-muted">
            <CalendarDays className="h-4 w-4 text-fg-faint" />
            {dateLabel}
          </span>

          <div className="seg" role="tablist" aria-label="Event kind">
            <Tab active={mode === "smart"} onClick={() => setMode("smart")}>
              <Sparkles className="h-3.5 w-3.5" /> Smart
            </Tab>
            <Tab active={mode === "basic"} onClick={() => setMode("basic")}>
              Basic
            </Tab>
            <Tab active={mode === "all"} onClick={() => setMode("all")}>
              All
            </Tab>
          </div>

          {mode !== "basic" && (
            <div className="seg" role="group" aria-label="Object type">
              <TypeBtn active={typeF === "all"} onClick={() => setTypeF("all")} label="All types">
                <Activity className="h-4 w-4" />
              </TypeBtn>
              <TypeBtn active={typeF === "person"} onClick={() => setTypeF("person")} label="People">
                <PersonStanding className="h-4 w-4" />
              </TypeBtn>
              <TypeBtn active={typeF === "vehicle"} onClick={() => setTypeF("vehicle")} label="Vehicles">
                <Car className="h-4 w-4" />
              </TypeBtn>
              <TypeBtn active={typeF === "animal"} onClick={() => setTypeF("animal")} label="Animals">
                <PawPrint className="h-4 w-4" />
              </TypeBtn>
              <TypeBtn active={typeF === "package"} onClick={() => setTypeF("package")} label="Packages">
                <Package className="h-4 w-4" />
              </TypeBtn>
              <TypeBtn active={typeF === "audio"} onClick={() => setTypeF("audio")} label="Audio">
                <Waves className="h-4 w-4" />
              </TypeBtn>
            </div>
          )}

          <span className="ml-auto text-xs tabular-nums text-fg-faint">
            {filtered.length} {filtered.length === 1 ? "event" : "events"}
          </span>
        </div>

        {filtered.length === 0 ? (
          <div style={{ height: h }} className="px-4 pb-4">
            <div className="inline-flex max-w-full items-center gap-2.5 rounded-md bg-ink-750 px-3 py-2 text-sm text-fg">
              <Info className="h-4 w-4 shrink-0 text-brand" />
              <span>Nothing matches this filter for the day.</span>
            </div>
          </div>
        ) : (
          <div
            style={{ height: h }}
            className={
              tall
                ? "flex flex-wrap content-start gap-2 overflow-y-auto px-4 pb-4"
                : "flex items-start gap-2 overflow-x-auto px-4 pb-3"
            }
          >
            {filtered.map((e) => (
              <button
                key={e.id}
                onClick={() => onSelect(e.startTs)}
                className="group relative aspect-video w-[144px] shrink-0 overflow-hidden rounded-md bg-ink-950 ring-1 ring-inset ring-white/[0.06] transition-shadow hover:ring-2 hover:ring-brand focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand"
              >
                {e.thumbUrl ? (
                  <img src={e.thumbUrl} alt="" className="h-full w-full object-cover" />
                ) : (
                  <div className="grid h-full w-full place-items-center text-fg-faint">
                    <TypeIcon e={e} className="h-5 w-5" />
                  </div>
                )}
                <div className="absolute inset-x-0 top-0 h-7 bg-gradient-to-b from-black/70 to-transparent" />
                <span className="absolute left-1.5 top-1 text-[11px] font-medium text-white drop-shadow">
                  {fmtRelative(e.startTs)}
                </span>
                <div className="osd-surface absolute bottom-1 left-1 grid h-5 w-5 place-items-center rounded">
                  <TypeIcon e={e} className="h-3 w-3 text-white" />
                </div>
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function Tab({ children, active, onClick }: { children: React.ReactNode; active?: boolean; onClick: () => void }) {
  return (
    <button
      role="tab"
      aria-selected={!!active}
      onClick={onClick}
      className={`seg-item gap-1.5 ${active ? "seg-item-active font-medium" : ""}`}
    >
      {children}
    </button>
  );
}

function TypeBtn({
  children,
  active,
  onClick,
  label,
  disabled,
}: {
  children: React.ReactNode;
  active: boolean;
  onClick: () => void;
  label: string;
  disabled?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      title={label}
      aria-pressed={active}
      className={`seg-item w-8 px-0 ${active ? "seg-item-active" : ""} ${disabled ? "opacity-40" : ""}`}
    >
      {children}
    </button>
  );
}
