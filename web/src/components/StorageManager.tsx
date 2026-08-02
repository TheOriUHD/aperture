import { useEffect, useState } from "react";
import { Trash2, RefreshCw, FolderCog } from "lucide-react";
import { ConfirmDialog } from "./ui/ConfirmDialog";
import { useStorage, useSetStorageConfig } from "../hooks/useStorage";
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

const CAM_COLORS = ["#1b9df0", "#37b0f7", "#1583cf", "#5bc0fa", "#0e6fb0", "#79cdfb", "#2a6f9e"];
const OTHER_COLOR = "#3a4150";
const FREE_COLOR = "#161922";
const GiB = 1024 ** 3;
const TiB = 1024 ** 4;

function startOfMonth() {
  const d = new Date();
  return new Date(d.getFullYear(), d.getMonth(), 1).getTime();
}
function toLocalInput(ms: number) {
  const d = new Date(ms - new Date().getTimezoneOffset() * 60000);
  return d.toISOString().slice(0, 16);
}

function Donut({
  segments,
  centerLabel,
  centerSub,
  size = 168,
  thickness = 22,
}: {
  segments: { label: string; value: number; color: string }[];
  centerLabel: string;
  centerSub: string;
  size?: number;
  thickness?: number;
}) {
  const r = (size - thickness) / 2;
  const c = 2 * Math.PI * r;
  const total = segments.reduce((a, s) => a + s.value, 0) || 1;
  let offset = 0;
  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} className="shrink-0">
      <g transform={`rotate(-90 ${size / 2} ${size / 2})`}>
        {segments.map((s, i) => {
          const dash = (s.value / total) * c;
          const el = (
            <circle
              key={i}
              cx={size / 2}
              cy={size / 2}
              r={r}
              fill="none"
              stroke={s.color}
              strokeWidth={thickness}
              strokeDasharray={`${dash} ${c - dash}`}
              strokeDashoffset={-offset}
            />
          );
          offset += dash;
          return el;
        })}
      </g>
      <text x="50%" y="47%" textAnchor="middle" className="fill-fg" style={{ fontSize: 22, fontWeight: 600 }}>
        {centerLabel}
      </text>
      <text x="50%" y="60%" textAnchor="middle" className="fill-fg-faint" style={{ fontSize: 11 }}>
        {centerSub}
      </text>
    </svg>
  );
}

export function StorageManager() {
  const { data: storage } = useStorage();
  const { data: cameras } = useCameras();
  const purge = usePurge();
  const reindex = useReindex();
  const setCfg = useSetStorageConfig();

  const [cameraId, setCameraId] = useState("");
  const [preset, setPreset] = useState<Preset>("24h");
  const [customFrom, setCustomFrom] = useState(() => toLocalInput(Date.now() - 24 * 3600_000));
  const [customTo, setCustomTo] = useState(() => toLocalInput(Date.now()));
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [result, setResult] = useState<PurgeResult | null>(null);
  const [reindexMsg, setReindexMsg] = useState("");

  // Config form state
  const [capValue, setCapValue] = useState("");
  const [capUnit, setCapUnit] = useState<"GB" | "TB">("TB");
  const [path, setPath] = useState("");
  const [cfgMsg, setCfgMsg] = useState("");
  const [cfgErr, setCfgErr] = useState(false);

  useEffect(() => {
    if (!storage) return;
    if (storage.capBytes > 0) {
      const tb = storage.capBytes / TiB;
      if (tb >= 1) {
        setCapUnit("TB");
        setCapValue(String(+tb.toFixed(2)));
      } else {
        setCapUnit("GB");
        setCapValue(String(+(storage.capBytes / GiB).toFixed(1)));
      }
    } else {
      setCapValue("");
    }
    setPath(storage.path === storage.defaultPath ? "" : storage.path);
  }, [storage]);

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

  async function saveCfg() {
    setCfgMsg("");
    setCfgErr(false);
    const v = parseFloat(capValue);
    const capBytes = capValue.trim() && v > 0 ? Math.round(v * (capUnit === "TB" ? TiB : GiB)) : 0;
    try {
      await setCfg.mutateAsync({ capBytes, path: path.trim() });
      setCfgMsg("Saved.");
    } catch (e) {
      setCfgErr(true);
      setCfgMsg((e as Error).message || "Could not save.");
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

  const allocated = storage?.allocatedBytes ?? 0;
  const hasAllocation = allocated > 0;
  const appBytes = storage?.appBytes ?? 0;
  const used = storage?.usedBytes ?? 0;
  const freeInAlloc = storage?.freeInAllocationBytes ?? 0;
  const legendTotal = hasAllocation ? allocated : Math.max(1, used);
  const usedPct = hasAllocation ? Math.round((used / allocated) * 100) : 0;
  const segments = storage
    ? [
        ...storage.cameras.map((c, i) => ({ label: c.name, value: c.bytes, color: CAM_COLORS[i % CAM_COLORS.length] })),
        { label: "Aperture app files", value: appBytes, color: OTHER_COLOR },
        ...(hasAllocation ? [{ label: "Free", value: freeInAlloc, color: FREE_COLOR }] : []),
      ]
    : [];

  return (
    <div className="panel">
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-sm font-medium text-fg">Storage</h2>
        <button
          className="btn-subtle h-8"
          disabled={reindex.isPending}
          onClick={doReindex}
          title="Index any footage on disk the database lost track of"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${reindex.isPending ? "animate-spin" : ""}`} />
          {reindex.isPending ? "Scanning…" : "Re-scan recordings"}
        </button>
      </div>
      {reindexMsg && <p className="text-xs text-online mb-3">{reindexMsg}</p>}

      {/* Donut + legend */}
      {storage && (
        <div className="flex flex-col sm:flex-row items-center gap-5 mb-4">
          <Donut
            segments={segments}
            centerLabel={hasAllocation ? `${usedPct}%` : formatBytes(used)}
            centerSub={hasAllocation ? `of ${formatBytes(allocated)}` : "used"}
          />
          <div className="flex-1 w-full space-y-1.5">
            {storage.cameras.map((c, i) => (
              <LegendRow
                key={c.cameraId}
                color={CAM_COLORS[i % CAM_COLORS.length]}
                label={c.name}
                value={c.bytes}
                total={legendTotal}
              />
            ))}
            <LegendRow color={OTHER_COLOR} label="Aperture app files" value={appBytes} total={legendTotal} />
            {hasAllocation && (
              <LegendRow color={FREE_COLOR} label="Free in allocation" value={freeInAlloc} total={legendTotal} ring />
            )}
            <div className="border-t border-line mt-1 pt-1.5 flex justify-between text-xs text-fg-muted">
              <span>{hasAllocation ? "Allocated to Aperture" : "Used by Aperture"}</span>
              <span className="tabular-nums">{formatBytes(hasAllocation ? allocated : used)}</span>
            </div>
            {!hasAllocation && (
              <p className="text-[11px] text-amber pt-1">Set an allocation below so usage is capped and shown clearly.</p>
            )}
          </div>
        </div>
      )}

      {/* Storage limit + location */}
      <div className="border-t border-line pt-4">
        <p className="flex items-center gap-1.5 text-xs font-medium text-fg-muted mb-2.5">
          <FolderCog className="h-3.5 w-3.5" />
          Allocation &amp; location
        </p>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className="label">Allocated to Aperture</label>
            <div className="flex gap-2">
              <input
                className="input"
                type="number"
                min={0}
                placeholder="Unlimited"
                value={capValue}
                onChange={(e) => setCapValue(e.target.value)}
              />
              <select className="input w-20" value={capUnit} onChange={(e) => setCapUnit(e.target.value as "GB" | "TB")}>
                <option value="GB">GB</option>
                <option value="TB">TB</option>
              </select>
            </div>
            <p className="text-[11px] text-fg-faint mt-1">
              Aperture uses up to this. Oldest footage is auto-deleted when you exceed it — no restart needed.
            </p>
          </div>
          <div>
            <label className="label">Recordings path</label>
            <input
              className="input font-mono text-xs"
              placeholder={storage?.defaultPath || "/recordings"}
              value={path}
              onChange={(e) => setPath(e.target.value)}
            />
            <p className="text-[11px] text-fg-faint mt-1">
              Must be writable in the container (mount a disk or network share there). New footage moves here.
            </p>
          </div>
        </div>
        <div className="flex items-center gap-3 mt-3">
          <button className="btn-primary h-8" disabled={setCfg.isPending} onClick={saveCfg}>
            {setCfg.isPending ? "Saving…" : "Save"}
          </button>
          {cfgMsg && <span className={`text-xs ${cfgErr ? "text-rec" : "text-online"}`}>{cfgMsg}</span>}
        </div>
      </div>

      {/* delete footage */}
      <div className="border-t border-line pt-4 mt-4">
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
              <input type="datetime-local" className="input" value={customFrom} onChange={(e) => setCustomFrom(e.target.value)} />
            </div>
            <div>
              <label className="label">To</label>
              <input type="datetime-local" className="input" value={customTo} onChange={(e) => setCustomTo(e.target.value)} />
            </div>
          </div>
        )}
        <div className="flex items-center gap-3">
          <button className="btn-danger" disabled={!validRange || purge.isPending} onClick={() => setConfirmOpen(true)}>
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
        message={`Permanently delete recordings for ${camLabel} — ${presetLabel[preset]}. This cannot be undone.`}
        confirmLabel="Delete footage"
        onConfirm={doPurge}
        onCancel={() => setConfirmOpen(false)}
      />
    </div>
  );
}

function LegendRow({
  color,
  label,
  value,
  total,
  ring,
}: {
  color: string;
  label: string;
  value: number;
  total: number;
  ring?: boolean;
}) {
  const pct = total > 0 ? Math.round((value / total) * 100) : 0;
  return (
    <div className="flex items-center gap-2 text-xs">
      <span
        className="h-2.5 w-2.5 rounded-sm shrink-0"
        style={{ backgroundColor: color, border: ring ? "1px solid #2a2f3a" : undefined }}
      />
      <span className="flex-1 truncate text-fg-muted">{label}</span>
      <span className="text-fg-faint tabular-nums w-10 text-right">{pct}%</span>
      <span className="text-fg-muted tabular-nums w-16 text-right">{formatBytes(value)}</span>
    </div>
  );
}
