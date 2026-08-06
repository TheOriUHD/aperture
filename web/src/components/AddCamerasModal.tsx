import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { X, Search, Cctv } from "lucide-react";
import { api } from "../lib/api";
import { useScrollLock } from "../hooks/useScrollLock";
import { fmtRelative } from "../lib/format";
import { gridLayout } from "./MultiCamGrid";
import type { Camera, MotionEvent } from "../lib/types";

// "Add Cameras to Playback": pick a set of cameras for synced multi-view. Shows a
// live layout preview, a searchable table with each camera's last event, and —
// when a multi-view is already running — a red button to end it.
export function AddCamerasModal({
  open,
  cameras,
  initial,
  active,
  onApply,
  onEnd,
  onClose,
}: {
  open: boolean;
  cameras: Camera[];
  initial: string[];
  active: boolean; // a multi-view is currently running (show "End multi-view")
  onApply: (ids: string[]) => void;
  onEnd: () => void;
  onClose: () => void;
}) {
  useScrollLock(open);
  const [sel, setSel] = useState<string[]>(initial);
  const [q, setQ] = useState("");

  useEffect(() => {
    if (open) {
      setSel(initial);
      setQ("");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Last event per camera (7-day lookback across all cameras, one query).
  const { data: events } = useQuery({
    queryKey: ["events-recent"],
    queryFn: () =>
      api.get<MotionEvent[]>(`/api/events?from=${Date.now() - 7 * 864e5}&to=${Date.now()}&limit=500`),
    enabled: open,
    staleTime: 60_000,
  });
  const lastByCam = useMemo(() => {
    const m: Record<string, number> = {};
    for (const e of events ?? []) if (!m[e.cameraId] || e.startTs > m[e.cameraId]) m[e.cameraId] = e.startTs;
    return m;
  }, [events]);

  if (!open) return null;

  const filtered = cameras.filter((c) => c.name.toLowerCase().includes(q.toLowerCase()));
  const allOn = filtered.length > 0 && filtered.every((c) => sel.includes(c.id));
  const toggle = (id: string) => setSel((s) => (s.includes(id) ? s.filter((x) => x !== id) : [...s, id]));
  const toggleAll = () =>
    setSel((s) => (allOn ? s.filter((id) => !filtered.some((c) => c.id === id)) : [...new Set([...s, ...filtered.map((c) => c.id)])]));

  // Layout preview — the exact layout the multi-view will use for this selection.
  const previewCams = cameras.filter((c) => sel.includes(c.id));
  const pv = gridLayout(previewCams.length);

  return (
    <div className="fixed inset-0 z-[70] grid place-items-center p-4">
      <div className="absolute inset-0 bg-black/70 animate-fade-in" onClick={onClose} />
      <div className="relative flex max-h-[88vh] w-full max-w-2xl flex-col overflow-hidden rounded-xl border border-line bg-ink-900 shadow-pop animate-fade-in">
        <div className="flex items-center justify-between px-5 py-4">
          <h2 className="text-lg font-semibold text-fg">Add Cameras to Playback</h2>
          <button onClick={onClose} className="btn-icon h-8 w-8" aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* live layout preview — mirrors the real multi-view layout exactly */}
        <div className="px-5">
          {previewCams.length === 0 ? (
            // No cameras yet: a 2×2 divider filler showing where they'll go.
            <div className="grid aspect-video grid-cols-2 grid-rows-2 gap-0.5 overflow-hidden rounded-lg bg-ink-850">
              {[0, 1, 2, 3].map((i) => (
                <div key={i} className="bg-ink-800" />
              ))}
            </div>
          ) : (
            <div className="grid aspect-video gap-0.5 overflow-hidden rounded-lg bg-ink-850" style={pv.style}>
              {previewCams.map((c, i) => (
                <div key={c.id} className="relative min-h-0 bg-ink-800" style={pv.cellStyle(i)}>
                  <img
                    src={`/api/v1/cameras/${c.id}/snapshot.jpg`}
                    alt=""
                    className="absolute inset-0 h-full w-full object-contain"
                    onError={(e) => (e.currentTarget.style.display = "none")}
                  />
                </div>
              ))}
            </div>
          )}
        </div>

        {/* searchable table */}
        <div className="m-5 flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-line">
          <div className="p-3">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-faint" />
              <input
                className="input h-10 pl-9"
                placeholder="Search Camera Name"
                value={q}
                onChange={(e) => setQ(e.target.value)}
              />
            </div>
          </div>
          <div className="grid grid-cols-[auto_1fr_auto] items-center gap-3 border-b border-line px-4 py-2 text-sm font-medium text-fg">
            <button
              onClick={toggleAll}
              className={`grid h-5 w-5 place-items-center rounded ${
                allOn ? "bg-brand text-white" : "border border-line-strong"
              }`}
              aria-label="Select all"
            >
              {allOn && <span className="text-xs">−</span>}
            </button>
            <span>Camera</span>
            <span className="text-fg-muted">Last Event</span>
          </div>
          <div className="min-h-0 flex-1 overflow-auto">
            {filtered.map((c) => {
              const on = sel.includes(c.id);
              return (
                <button
                  key={c.id}
                  onClick={() => toggle(c.id)}
                  className="grid w-full grid-cols-[auto_1fr_auto] items-center gap-3 border-b border-line/60 px-4 py-3 text-left hover:bg-ink-850"
                >
                  <span
                    className={`grid h-5 w-5 place-items-center rounded ${
                      on ? "bg-brand text-white" : "border border-line-strong"
                    }`}
                  >
                    {on && <span className="text-xs">✓</span>}
                  </span>
                  <span className="flex items-center gap-2.5">
                    <span className={`h-2 w-2 rounded-full ${c.enabled ? "bg-online" : "bg-fg-faint"}`} />
                    <Cctv className="h-4 w-4 text-fg-muted" />
                    <span className="truncate text-sm text-fg">{c.name}</span>
                  </span>
                  <span className="text-sm text-fg-muted">
                    {lastByCam[c.id] ? fmtRelative(lastByCam[c.id]) : "—"}
                  </span>
                </button>
              );
            })}
          </div>
        </div>

        {/* footer */}
        <div className="flex items-center gap-2 px-5 pb-5">
          {active && (
            <button className="btn-danger" onClick={onEnd}>
              End multi-view
            </button>
          )}
          <div className="ml-auto flex items-center gap-2">
            <button className="btn-ghost" onClick={onClose}>
              Cancel
            </button>
            <button className="btn-primary" onClick={() => onApply(sel)} disabled={sel.length === 0}>
              {active ? "Apply" : "Add"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
