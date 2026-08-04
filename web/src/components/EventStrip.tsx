import { useRef, useState } from "react";
import { Activity, Footprints, Car, PawPrint, PersonStanding, Sparkles, Waves } from "lucide-react";
import type { MotionEvent } from "../lib/types";
import { fmtRelative } from "../lib/format";

type Mode = "smart" | "basic" | "all";
type TypeFilter = "all" | "person" | "vehicle" | "animal" | "audio";

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
  const ls = labelsOf(e);
  if (ls.some((l) => l.includes("person") || l.includes("people"))) return "person";
  if (ls.some((l) => VEHICLE.some((v) => l.includes(v)))) return "vehicle";
  if (ls.some((l) => ANIMAL.some((a) => l.includes(a)))) return "animal";
  return "motion";
}

function TypeIcon({ e, className }: { e: MotionEvent; className?: string }) {
  const t = eventType(e);
  const Icon =
    t === "person" ? PersonStanding : t === "vehicle" ? Car : t === "animal" ? PawPrint : t === "audio" ? Waves : Footprints;
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
    <div className="shrink-0 border-t border-line bg-ink-900">
      {/* drag handle */}
      <div
        onPointerDown={onDown}
        onPointerMove={onMove}
        onPointerUp={onUp}
        className="group flex h-2 cursor-ns-resize items-center justify-center"
        title="Drag to resize"
      >
        <div className="h-1 w-8 rounded-full bg-line-strong group-hover:bg-fg-faint" />
      </div>

      {/* filter row */}
      <div className="flex items-center gap-3 px-4 pb-2">
        <div className="flex items-center gap-1 text-xs">
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
          <>
            <div className="h-4 w-px bg-line" />
            <div className="flex items-center gap-0.5">
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
              <TypeBtn active={typeF === "audio"} onClick={() => setTypeF("audio")} label="Audio">
                <Waves className="h-4 w-4" />
              </TypeBtn>
            </div>
          </>
        )}

        <span className="ml-auto text-xs text-fg-faint">{dateLabel}</span>
        <span className="text-xs text-fg-faint tabular-nums">
          {filtered.length} {filtered.length === 1 ? "event" : "events"}
        </span>
      </div>

      {filtered.length === 0 ? (
        <div style={{ height: h }} className="grid place-items-center">
          <div className="flex flex-col items-center gap-1.5 text-fg-faint">
            <Activity className="h-5 w-5" />
            <span className="text-xs">Nothing matches this filter for the day</span>
          </div>
        </div>
      ) : (
        <div
          style={{ height: h }}
          className={
            tall
              ? "flex flex-wrap content-start gap-2 overflow-y-auto px-3 pb-3"
              : "flex items-center gap-2 overflow-x-auto px-3 pb-2"
          }
        >
          {filtered.map((e) => (
            <button
              key={e.id}
              onClick={() => onSelect(e.startTs)}
              className="group relative aspect-video w-[136px] shrink-0 overflow-hidden rounded-md border border-line bg-ink-800 hover:border-brand"
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
              <div className="absolute bottom-1 left-1.5 grid h-5 w-5 place-items-center rounded bg-black/55">
                <TypeIcon e={e} className="h-3 w-3 text-white" />
              </div>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function Tab({ children, active, onClick }: { children: React.ReactNode; active?: boolean; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className={`flex items-center gap-1 rounded-md px-2.5 py-1 ${
        active ? "text-brand font-medium" : "text-fg-muted hover:text-fg"
      }`}
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
      className={`grid h-7 w-7 place-items-center rounded-md ${
        active ? "bg-brand/15 text-brand" : "text-fg-muted hover:text-fg hover:bg-ink-700"
      } ${disabled ? "opacity-40" : ""}`}
    >
      {children}
    </button>
  );
}
