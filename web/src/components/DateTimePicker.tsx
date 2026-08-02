import { useState } from "react";
import { ChevronLeft, ChevronRight, ChevronUp, ChevronDown } from "lucide-react";

const WEEKDAYS = ["Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"];
const MONTHS = [
  "January", "February", "March", "April", "May", "June",
  "July", "August", "September", "October", "November", "December",
];

// A compact, self-contained calendar + time selector — deliberately NOT the
// native picker. Controlled: `value` is unix ms, `onChange` gets the new instant.
// Clamps to [min, max] when provided (e.g. max = now to block the future).
export function DateTimePicker({
  value,
  onChange,
  min,
  max,
}: {
  value: number;
  onChange: (ms: number) => void;
  min?: number;
  max?: number;
}) {
  const sel = new Date(value);
  const [view, setView] = useState(() => new Date(sel.getFullYear(), sel.getMonth(), 1));

  const clamp = (ms: number) => {
    if (min != null && ms < min) return min;
    if (max != null && ms > max) return max;
    return ms;
  };

  // Monday-first grid: how many leading blanks before day 1.
  const firstDow = (new Date(view.getFullYear(), view.getMonth(), 1).getDay() + 6) % 7;
  const daysInMonth = new Date(view.getFullYear(), view.getMonth() + 1, 0).getDate();
  const today = new Date();
  const isToday = (d: number) =>
    today.getFullYear() === view.getFullYear() &&
    today.getMonth() === view.getMonth() &&
    today.getDate() === d;
  const isSelected = (d: number) =>
    sel.getFullYear() === view.getFullYear() &&
    sel.getMonth() === view.getMonth() &&
    sel.getDate() === d;
  const dayDisabled = (d: number) => {
    const start = new Date(view.getFullYear(), view.getMonth(), d, 0, 0, 0).getTime();
    const end = start + 24 * 3600_000 - 1;
    return (min != null && end < min) || (max != null && start > max);
  };

  function pickDay(d: number) {
    const nd = new Date(value);
    nd.setFullYear(view.getFullYear(), view.getMonth(), d);
    onChange(clamp(nd.getTime()));
  }
  function bumpMonth(dir: number) {
    setView((v) => new Date(v.getFullYear(), v.getMonth() + dir, 1));
  }
  function bumpTime(unit: "h" | "m", dir: number) {
    const nd = new Date(value);
    if (unit === "h") nd.setHours(nd.getHours() + dir);
    else nd.setMinutes(nd.getMinutes() + dir);
    nd.setSeconds(0, 0);
    onChange(clamp(nd.getTime()));
  }

  const cells: (number | null)[] = [
    ...Array(firstDow).fill(null),
    ...Array.from({ length: daysInMonth }, (_, i) => i + 1),
  ];

  return (
    <div className="w-[260px] select-none rounded-lg border border-line bg-ink-850 p-3 shadow-pop">
      {/* Month header */}
      <div className="mb-2 flex items-center justify-between">
        <button className="btn-icon h-7 w-7" onClick={() => bumpMonth(-1)} aria-label="Previous month">
          <ChevronLeft className="h-4 w-4" />
        </button>
        <span className="text-sm font-medium text-fg">
          {MONTHS[view.getMonth()]} {view.getFullYear()}
        </span>
        <button className="btn-icon h-7 w-7" onClick={() => bumpMonth(1)} aria-label="Next month">
          <ChevronRight className="h-4 w-4" />
        </button>
      </div>

      {/* Weekday row */}
      <div className="grid grid-cols-7 gap-0.5 text-center text-[10px] text-fg-faint">
        {WEEKDAYS.map((w) => (
          <div key={w} className="py-1">{w}</div>
        ))}
      </div>

      {/* Day grid */}
      <div className="grid grid-cols-7 gap-0.5">
        {cells.map((d, i) =>
          d == null ? (
            <div key={i} />
          ) : (
            <button
              key={i}
              disabled={dayDisabled(d)}
              onClick={() => pickDay(d)}
              className={`aspect-square rounded-md text-xs transition-colors disabled:opacity-25 ${
                isSelected(d)
                  ? "bg-brand text-white"
                  : isToday(d)
                    ? "text-brand hover:bg-ink-700"
                    : "text-fg-muted hover:bg-ink-700 hover:text-fg"
              }`}
            >
              {d}
            </button>
          ),
        )}
      </div>

      {/* Time steppers */}
      <div className="mt-3 flex items-center justify-center gap-1 border-t border-line pt-3">
        <TimeField label="Hour" digits={String(sel.getHours()).padStart(2, "0")} onBump={(dir) => bumpTime("h", dir)} />
        <span className="pb-0.5 text-lg font-medium text-fg-faint">:</span>
        <TimeField label="Minute" digits={String(sel.getMinutes()).padStart(2, "0")} onBump={(dir) => bumpTime("m", dir)} />
      </div>
    </div>
  );
}

function TimeField({ label, digits, onBump }: { label: string; digits: string; onBump: (dir: number) => void }) {
  return (
    <div className="flex flex-col items-center">
      <button className="text-fg-faint hover:text-fg" aria-label={`${label} up`} onClick={() => onBump(1)}>
        <ChevronUp className="h-4 w-4" />
      </button>
      <span className="w-9 text-center text-lg font-mono tabular-nums text-fg">{digits}</span>
      <button className="text-fg-faint hover:text-fg" aria-label={`${label} down`} onClick={() => onBump(-1)}>
        <ChevronDown className="h-4 w-4" />
      </button>
    </div>
  );
}
