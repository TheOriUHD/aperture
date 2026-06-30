import { useState } from "react";
import { Trash2, HardDrive } from "lucide-react";
import { ConfirmDialog } from "./ui/ConfirmDialog";
import { RefreshCw } from "lucide-react";
import { useStorage } from "../hooks/useStorage";
import { useCameras } from "../hooks/useCameras";
import { usePurge, useReindex, type PurgeResult } from "../hooks/useSystem";
import { formatBytes } from "../lib/format";

type Preset = "24h" | "7d" | "month" | "all" | "custom";

const presetLabel: Record<Preset, string> = {
  "24h": "Last 24 hours",
  "7d": "Last 7 days",
  month: "This month",
  all: "All footage",
  custom: "Custom range",
};

function startOfMonth() {
  const d = new Date();
  return new Date(d.getFullYear(), d.getMonth(), 1).getTime();
}

function toLocalInput(ms: number) {
  const d = new Date(ms - new Date().getTimezoneOffset() * 60000);
  return d.toISOString().slice(0, 16);
}

export function StorageManager() {
  const { data: storage } = useStorage();
  const { data: cameras } = useCameras();
  const purge = usePurge();
  const reindex = useReindex();
  const [reindexMsg, setReindexMsg] = useState("");

  const [cameraId, setCameraId] = useState("");
  const [preset, setPreset] = useState<Preset>("24h");
  const [customFrom, setCustomFrom] = useState(() => toLocalInput(Date.now() - 24 * 3600_000));
  const [customTo, setCustomTo] = useState(() => toLocalInput(Date.now()));
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [result, setResult] = useState<PurgeResult | null>(null);

  function range(): { from: number; to: number } {
    const now = Date.now();
    switch (preset) {
      case "24h":
        return { from: now - 24 * 3600_000, to: now };
      case "7d":
        return { from: now - 7 * 24 * 3600_000, to: now };
      case "month":
        return { from: startOfMonth(), to: now };
      case "all":
        return { from: 0, to: now };
      case "custom":
        return { from: new Date(customFrom).getTime(), to: new Date(customTo).getTime() };
    }
  }

  const camLabel = cameraId ? cameras?.find((c) => c.id === cameraId)?.name : "all cameras";
  const r = range();
  const validRange = r.to > r.from;

  async function doPurge() {
    try {
      const res = await purge.mutateAsync({ cameraId: cameraId || undefined, from: r.from, to: r.to });
      setResult(res);
    } catch {
      /* ignore */
    } finally {
      setConfirmOpen(false);
    }
  }

  const usedPct =
    storage && storage.diskTotalBytes > 0
      ? Math.min(100, Math.round((storage.recordedBytes / storage.diskTotalBytes) * 100))
      : 0;

  async function doReindex() {
    setReindexMsg("");
    try {
      const res = await reindex.mutateAsync();
      setReindexMsg(
        res.recovered > 0
          ? `Recovered ${res.recovered.toLocaleString()} orphaned segments.`
          : "Everything on disk is already indexed.",
      );
    } catch {
      setReindexMsg("Re-scan failed.");
    }
  }

  return (
    <div className="panel">
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-sm font-medium text-fg">Storage</h2>
        <button className="btn-subtle h-8" disabled={reindex.isPending} onClick={doReindex} title="Index any footage on disk the database lost track of">
          <RefreshCw className={`h-3.5 w-3.5 ${reindex.isPending ? "animate-spin" : ""}`} />
          {reindex.isPending ? "Scanning…" : "Re-scan recordings"}
        </button>
      </div>
      {reindexMsg && <p className="text-xs text-online mb-3">{reindexMsg}</p>}

      {/* overall usage */}
      <div className="mb-4">
        <div className="flex items-center justify-between text-xs mb-1.5">
          <span className="flex items-center gap-1.5 text-fg-muted">
            <HardDrive className="h-3.5 w-3.5" />
            Recordings
          </span>
          <span className="text-fg-muted">
            {storage ? `${formatBytes(storage.recordedBytes)} of ${formatBytes(storage.diskTotalBytes)}` : "…"}
            {storage && <span className="text-fg-faint"> · {formatBytes(storage.diskFreeBytes)} free</span>}
          </span>
        </div>
        <div className="h-2 rounded-full bg-ink-850 overflow-hidden">
          <div className="h-full bg-brand-deep" style={{ width: `${usedPct}%` }} />
        </div>
      </div>

      {/* per-camera */}
      {storage && storage.cameras.length > 0 && (
        <div className="space-y-1.5 mb-4">
          {storage.cameras.map((c) => {
            const pct =
              storage.recordedBytes > 0 ? Math.round((c.bytes / storage.recordedBytes) * 100) : 0;
            return (
              <div key={c.cameraId} className="flex items-center gap-3 text-xs">
                <span className="w-28 truncate text-fg-muted">{c.name}</span>
                <div className="flex-1 h-1.5 rounded-full bg-ink-850 overflow-hidden">
                  <div className="h-full bg-brand-deep/70" style={{ width: `${pct}%` }} />
                </div>
                <span className="w-16 text-right text-fg-faint">{formatBytes(c.bytes)}</span>
              </div>
            );
          })}
        </div>
      )}

      {/* delete footage */}
      <div className="border-t border-line pt-4">
        <p className="text-xs font-medium text-fg-muted mb-2.5">Delete footage</p>
        <div className="grid grid-cols-2 gap-3 mb-3">
          <div>
            <label className="label">Camera</label>
            <select className="input" value={cameraId} onChange={(e) => setCameraId(e.target.value)}>
              <option value="">All cameras</option>
              {cameras?.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="label">Range</label>
            <select className="input" value={preset} onChange={(e) => setPreset(e.target.value as Preset)}>
              {(["24h", "7d", "month", "all", "custom"] as Preset[]).map((p) => (
                <option key={p} value={p}>
                  {presetLabel[p]}
                </option>
              ))}
            </select>
          </div>
        </div>
        {preset === "custom" && (
          <div className="grid grid-cols-2 gap-3 mb-3">
            <div>
              <label className="label">From</label>
              <input
                type="datetime-local"
                className="input"
                value={customFrom}
                onChange={(e) => setCustomFrom(e.target.value)}
              />
            </div>
            <div>
              <label className="label">To</label>
              <input
                type="datetime-local"
                className="input"
                value={customTo}
                onChange={(e) => setCustomTo(e.target.value)}
              />
            </div>
          </div>
        )}
        <div className="flex items-center gap-3">
          <button
            className="btn-danger"
            disabled={!validRange || purge.isPending}
            onClick={() => setConfirmOpen(true)}
          >
            <Trash2 className="h-4 w-4" />
            Delete footage
          </button>
          {result && (
            <span className="text-xs text-fg-muted">
              Freed {formatBytes(result.freedBytes)} ({result.deletedSegments} segments
              {result.deletedEvents ? `, ${result.deletedEvents} events` : ""})
            </span>
          )}
        </div>
      </div>

      <ConfirmDialog
        open={confirmOpen}
        danger
        busy={purge.isPending}
        title="Delete footage"
        message={`Permanently delete recordings for ${camLabel} — ${presetLabel[preset]}${
          preset === "custom" ? "" : ""
        }. This cannot be undone.`}
        confirmLabel="Delete footage"
        onConfirm={doPurge}
        onCancel={() => setConfirmOpen(false)}
      />
    </div>
  );
}
