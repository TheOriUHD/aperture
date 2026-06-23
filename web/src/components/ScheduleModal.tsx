import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { X } from "lucide-react";
import { api } from "../lib/api";

interface Window {
  weekday: number;
  startMin: number;
  endMin: number;
}

// Display rows Mon..Sun mapped to Go weekday (0=Sunday).
const DAYS = [
  { label: "Mon", wd: 1 },
  { label: "Tue", wd: 2 },
  { label: "Wed", wd: 3 },
  { label: "Thu", wd: 4 },
  { label: "Fri", wd: 5 },
  { label: "Sat", wd: 6 },
  { label: "Sun", wd: 0 },
];
const HOURS = Array.from({ length: 24 }, (_, h) => h);

function emptyGrid() {
  return DAYS.map(() => HOURS.map(() => false));
}

export function ScheduleModal({
  open,
  cameraId,
  cameraName,
  onClose,
}: {
  open: boolean;
  cameraId: string;
  cameraName: string;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const [grid, setGrid] = useState<boolean[][]>(emptyGrid);
  const [busy, setBusy] = useState(false);
  const paint = useRef<{ value: boolean } | null>(null);

  const { data: windows } = useQuery({
    queryKey: ["schedule", cameraId],
    queryFn: () => api.get<Window[]>(`/api/cameras/${cameraId}/schedule`),
    enabled: open && !!cameraId,
  });

  useEffect(() => {
    if (!open) return;
    const g = emptyGrid();
    for (const win of windows ?? []) {
      const row = DAYS.findIndex((d) => d.wd === win.weekday);
      if (row < 0) continue;
      const h0 = Math.floor(win.startMin / 60);
      const h1 = Math.floor((win.endMin - 1) / 60);
      for (let h = h0; h <= h1 && h < 24; h++) g[row][h] = true;
    }
    setGrid(g);
  }, [windows, open]);

  useEffect(() => {
    const stop = () => (paint.current = null);
    window.addEventListener("pointerup", stop);
    return () => window.removeEventListener("pointerup", stop);
  }, []);

  function apply(row: number, h: number, value: boolean) {
    setGrid((g) => {
      const next = g.map((r) => r.slice());
      next[row][h] = value;
      return next;
    });
  }
  function setAll(value: boolean) {
    setGrid(DAYS.map(() => HOURS.map(() => value)));
  }

  async function save() {
    const out: Window[] = [];
    grid.forEach((row, ri) => {
      let start = -1;
      for (let h = 0; h <= 24; h++) {
        const on = h < 24 && row[h];
        if (on && start < 0) start = h;
        if (!on && start >= 0) {
          out.push({ weekday: DAYS[ri].wd, startMin: start * 60, endMin: h * 60 });
          start = -1;
        }
      }
    });
    setBusy(true);
    try {
      await api.put(`/api/cameras/${cameraId}/schedule`, { windows: out });
      qc.invalidateQueries({ queryKey: ["schedule", cameraId] });
      onClose();
    } finally {
      setBusy(false);
    }
  }

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-[55] grid place-items-center p-4">
      <div className="absolute inset-0 bg-black/60 animate-fade-in" onClick={onClose} />
      <div className="relative w-full max-w-3xl card shadow-pop animate-fade-in">
        <div className="flex items-center justify-between px-5 h-14 border-b border-line">
          <div>
            <h2 className="text-base font-medium text-fg">Recording schedule</h2>
            <p className="text-xs text-fg-faint">{cameraName} · click or drag to paint windows</p>
          </div>
          <button className="btn-icon" onClick={onClose} aria-label="Close">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="p-5">
          <div className="select-none" style={{ touchAction: "none" }}>
            {/* hour header */}
            <div
              className="grid items-end mb-1 text-[10px] font-mono text-fg-faint"
              style={{ gridTemplateColumns: `36px repeat(24, 1fr)` }}
            >
              <div />
              {HOURS.map((h) => (
                <div key={h} className="text-center">
                  {h % 2 === 0 ? h : ""}
                </div>
              ))}
            </div>
            {/* day rows */}
            {DAYS.map((d, ri) => (
              <div
                key={d.label}
                className="grid items-center mb-1"
                style={{ gridTemplateColumns: `36px repeat(24, 1fr)` }}
              >
                <button
                  className="text-[11px] text-fg-muted hover:text-fg text-left pr-2"
                  onClick={() => {
                    const allOn = grid[ri].every(Boolean);
                    setGrid((g) => {
                      const next = g.map((r) => r.slice());
                      next[ri] = HOURS.map(() => !allOn);
                      return next;
                    });
                  }}
                  title="Toggle whole day"
                >
                  {d.label}
                </button>
                {HOURS.map((h) => (
                  <div
                    key={h}
                    onPointerDown={(e) => {
                      e.preventDefault();
                      const value = !grid[ri][h];
                      paint.current = { value };
                      apply(ri, h, value);
                    }}
                    onPointerEnter={() => {
                      if (paint.current) apply(ri, h, paint.current.value);
                    }}
                    className={`h-6 border-r border-ink-900 cursor-pointer transition-colors ${
                      grid[ri][h] ? "bg-brand hover:bg-[#2aa6f5]" : "bg-ink-850 hover:bg-ink-700"
                    } ${h === 0 ? "rounded-l-sm border-l border-ink-900" : ""} ${
                      h === 23 ? "rounded-r-sm" : ""
                    }`}
                  />
                ))}
              </div>
            ))}
          </div>

          <div className="flex items-center gap-2 mt-4">
            <button className="text-xs text-fg-muted hover:text-fg" onClick={() => setAll(true)}>
              Select all
            </button>
            <span className="text-line">·</span>
            <button className="text-xs text-fg-muted hover:text-fg" onClick={() => setAll(false)}>
              Clear
            </button>
            <span className="ml-auto text-xs text-fg-faint">Recording is on for the blue hours</span>
          </div>
        </div>

        <div className="flex justify-end gap-2 px-5 py-4 border-t border-line">
          <button className="btn-ghost" onClick={onClose}>
            Cancel
          </button>
          <button className="btn-primary" onClick={save} disabled={busy}>
            {busy ? "Saving…" : "Save schedule"}
          </button>
        </div>
      </div>
    </div>
  );
}
