import { useEffect, useMemo, useState, type CSSProperties, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useQuery } from "@tanstack/react-query";
import { X, Search, Cctv, Check, Minus } from "lucide-react";
import { api } from "../lib/api";
import { useScrollLock } from "../hooks/useScrollLock";
import { fmtRelative } from "../lib/format";
import { gridLayout } from "./MultiCamGrid";
import type { Camera, MotionEvent } from "../lib/types";

// Picks a set of cameras: a live layout preview, a searchable table with each
// camera's last event, and, while a selection is already in use, a red button to
// end it. The defaults are Playback's "Add Cameras to Playback" for synced
// multi-view; the dashboard reuses it to choose the cameras on its wall.
export function AddCamerasModal({
  open,
  cameras,
  initial,
  active = false,
  onApply,
  onEnd,
  onClose,
  title = "Add Cameras to Playback",
  applyLabel,
  endLabel = "End multi-view",
  ordered = false,
  preview,
  container,
}: {
  open: boolean;
  cameras: Camera[];
  initial: string[];
  active?: boolean; // a selection is currently in use (show the end button)
  onApply: (ids: string[]) => void;
  onEnd?: () => void;
  onClose: () => void;
  title?: string;
  /** Defaults to "Apply" while `active`, else "Add". */
  applyLabel?: string;
  endLabel?: string;
  /** Preview the selection in the order it was ticked instead of list order. */
  ordered?: boolean;
  /** Replaces the default multi-view layout preview. */
  preview?: (selected: Camera[]) => ReactNode;
  /** Renders the modal into this element, e.g. a fullscreen element, outside of
   * which nothing shows. Without it the modal renders in place. */
  container?: Element | null;
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
  const previewCams = ordered
    ? sel.map((id) => cameras.find((c) => c.id === id)).filter((c): c is Camera => !!c)
    : cameras.filter((c) => sel.includes(c.id));
  const pv = gridLayout(previewCams.length);

  const modal = (
    <div className="fixed inset-0 z-[70] grid place-items-center p-4">
      <div className="absolute inset-0 bg-black/60 animate-fade-in" onClick={onClose} />
      <div className="card relative flex max-h-[88dvh] w-full max-w-2xl flex-col overflow-hidden shadow-pop animate-fade-in">
        <div className="flex h-14 shrink-0 items-center justify-between border-b border-line px-5">
          <h2 className="text-base font-medium text-fg">{title}</h2>
          <button onClick={onClose} className="btn-icon" aria-label="Close">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex min-h-0 flex-1 flex-col gap-4 p-5">
          {/* live layout preview — mirrors the real multi-view layout exactly */}
          <div className="shrink-0">
            {preview ? (
              preview(previewCams)
            ) : previewCams.length === 0 ? (
              // No cameras yet: a 2×2 divider filler showing where they'll go.
              <div className="grid aspect-video max-h-[34dvh] w-full grid-cols-2 grid-rows-2 gap-0.5 overflow-hidden rounded-lg bg-ink-800">
                {[0, 1, 2, 3].map((i) => (
                  <div key={i} className="bg-ink-950" />
                ))}
              </div>
            ) : (
              <div
                className="grid aspect-video max-h-[34dvh] w-full gap-0.5 overflow-hidden rounded-lg bg-ink-800"
                style={pv.style}
              >
                {previewCams.map((c, i) => (
                  <PreviewTile key={c.id} camera={c} style={pv.cellStyle(i)} />
                ))}
              </div>
            )}
          </div>

          {/* searchable table */}
          <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-line">
            <div className="border-b border-line p-3">
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-faint" />
                <input
                  className="input pl-8"
                  placeholder="Search Camera Name"
                  value={q}
                  onChange={(e) => setQ(e.target.value)}
                />
              </div>
            </div>
            <div className="grid h-9 shrink-0 grid-cols-[auto_1fr_auto] items-center gap-3 border-b border-line px-4 text-[13px] font-semibold text-fg">
              <button
                onClick={toggleAll}
                className={`grid h-4 w-4 place-items-center rounded-[4px] transition-colors ${
                  allOn ? "bg-brand-solid text-white" : "border border-line-strong hover:border-fg-faint"
                }`}
                aria-label="Select all"
              >
                {allOn && <Minus className="h-3 w-3" strokeWidth={3} />}
              </button>
              <span>Camera</span>
              <span>Last Event</span>
            </div>
            <div className="min-h-0 flex-1 overflow-auto">
              {filtered.map((c) => {
                const on = sel.includes(c.id);
                return (
                  <button
                    key={c.id}
                    onClick={() => toggle(c.id)}
                    className={`grid h-10 w-full grid-cols-[auto_1fr_auto] items-center gap-3 border-b border-line px-4 text-left transition-colors last:border-b-0 ${
                      on ? "bg-brand-ink/60 hover:bg-brand-ink" : "hover:bg-white/[0.03]"
                    }`}
                  >
                    <span
                      className={`grid h-4 w-4 place-items-center rounded-[4px] ${
                        on ? "bg-brand-solid text-white" : "border border-line-strong"
                      }`}
                    >
                      {on && <Check className="h-3 w-3" strokeWidth={3} />}
                    </span>
                    <span className="flex min-w-0 items-center gap-2.5">
                      <span className={`h-2 w-2 shrink-0 rounded-full ${c.enabled ? "bg-online" : "bg-fg-faint"}`} />
                      <Cctv className="h-4 w-4 shrink-0 text-fg-muted" />
                      <span className="truncate text-sm text-fg">{c.name}</span>
                    </span>
                    <span className="text-sm tabular-nums text-fg-muted">
                      {lastByCam[c.id] ? fmtRelative(lastByCam[c.id]) : "—"}
                    </span>
                  </button>
                );
              })}
              {filtered.length === 0 && (
                <p className="px-4 py-8 text-center text-sm text-fg-faint">No cameras match “{q}”.</p>
              )}
            </div>
          </div>
        </div>

        {/* footer */}
        <div className="flex shrink-0 items-center gap-2 border-t border-line px-5 py-4">
          {active && onEnd && (
            <button className="btn-danger" onClick={onEnd}>
              {endLabel}
            </button>
          )}
          <span className="text-xs text-fg-faint">
            {sel.length} {sel.length === 1 ? "camera" : "cameras"} selected
          </span>
          <div className="ml-auto flex items-center gap-2">
            <button className="btn-ghost" onClick={onClose}>
              Cancel
            </button>
            <button className="btn-primary" onClick={() => onApply(sel)} disabled={sel.length === 0}>
              {applyLabel ?? (active ? "Apply" : "Add")}
            </button>
          </div>
        </div>
      </div>
    </div>
  );

  return container ? createPortal(modal, container) : modal;
}

/** One camera in a layout preview: its latest snapshot with the name over it. */
export function PreviewTile({
  camera,
  style,
  className,
}: {
  camera: Camera;
  style?: CSSProperties;
  className?: string;
}) {
  return (
    <div className={`relative min-h-0 bg-ink-950 ${className ?? ""}`} style={style}>
      <img
        src={`/api/v1/cameras/${camera.id}/snapshot.jpg`}
        alt=""
        className="absolute inset-0 h-full w-full object-contain"
        onError={(e) => (e.currentTarget.style.display = "none")}
      />
      <span className="osd-chip pointer-events-none absolute bottom-1.5 left-1.5 h-6 max-w-[80%] px-2 text-[11px]">
        <span className="truncate">{camera.name}</span>
      </span>
    </div>
  );
}
