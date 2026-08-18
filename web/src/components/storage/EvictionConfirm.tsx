import { AlertTriangle } from "lucide-react";
import { Modal } from "../ui/Modal";
import { formatBytes } from "../../lib/format";
import type { EvictionPreview } from "../../hooks/useStorageLocations";

// EvictionConfirm states, in plain numbers, exactly what a lower allocation will
// destroy — how much, how many recordings, which cameras and back to when. The
// figures come from the server (the same oldest-first walk the janitor performs),
// never from an estimate made here, so the dialog cannot promise something
// different from what actually happens.
export function EvictionConfirm({
  preview,
  busy,
  onCancel,
  onConfirm,
}: {
  preview: EvictionPreview | null;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  if (!preview) return null;
  const day = (ts: number) => (ts > 0 ? new Date(ts).toLocaleString() : "—");
  return (
    <Modal open onClose={onCancel} title="Delete footage to shrink this destination?">
      <div className="space-y-4">
        <div className="flex items-start gap-2.5 rounded-lg border border-rec/40 bg-rec/10 p-3">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-rec" />
          <div className="text-xs leading-relaxed text-fg">
            Lowering this limit deletes the oldest recordings straight away to fit.
            <strong className="text-fg"> This cannot be undone.</strong>
          </div>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <div className="rounded-lg border border-line bg-ink-850 p-3">
            <p className="text-[11px] uppercase tracking-wide text-fg-faint">Will be deleted</p>
            <p className="mt-0.5 text-lg font-semibold tabular-nums text-rec">{formatBytes(preview.evictBytes)}</p>
            <p className="text-[11px] text-fg-faint">{preview.evictSegments.toLocaleString()} recordings</p>
          </div>
          <div className="rounded-lg border border-line bg-ink-850 p-3">
            <p className="text-[11px] uppercase tracking-wide text-fg-faint">New limit</p>
            <p className="mt-0.5 text-lg font-semibold tabular-nums text-fg">
              {formatBytes(preview.proposedAllocationBytes)}
            </p>
            <p className="text-[11px] text-fg-faint">was {formatBytes(preview.currentAllocationBytes)}</p>
          </div>
        </div>

        <div>
          <p className="label mb-1.5">Footage from {day(preview.oldestTs)} up to {day(preview.newestTs)} goes</p>
          <div className="space-y-1">
            {preview.cameras.map((c) => (
              <div key={c.cameraId} className="flex items-center gap-2 text-xs">
                <span className="w-32 truncate text-fg">{c.name}</span>
                <span className="flex-1 text-fg-faint">{c.segments.toLocaleString()} recordings</span>
                <span className="tabular-nums text-fg-muted">{formatBytes(c.bytes)}</span>
              </div>
            ))}
          </div>
        </div>

        <div className="flex justify-end gap-2 pt-1">
          <button className="btn-ghost" onClick={onCancel} disabled={busy}>
            Keep my footage
          </button>
          <button className="btn-danger" onClick={onConfirm} disabled={busy}>
            {busy ? "Deleting…" : `Delete ${formatBytes(preview.evictBytes)}`}
          </button>
        </div>
      </div>
    </Modal>
  );
}
