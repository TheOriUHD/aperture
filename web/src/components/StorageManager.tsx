import { useState } from "react";
import { Trash2, RefreshCw, Plus, HardDrive, Server, Cloud, Lock, Pencil, Timer, PauseCircle, Video, ChevronDown } from "lucide-react";
import { ConfirmDialog } from "./ui/ConfirmDialog";
import { StorageSetupWizard } from "./storage/StorageSetupWizard";
import { EditLocationModal } from "./storage/EditLocationModal";
import { useStorage, type StorageLocation } from "../hooks/useStorage";
import { useStorageLocations, useDeleteLocation, useSetCameraDestinations } from "../hooks/useStorageLocations";
import { useCameras } from "../hooks/useCameras";
import { usePurge, useReindex, type PurgeResult } from "../hooks/useSystem";
import { ApiError } from "../lib/api";
import { formatBytes } from "../lib/format";
import { donutSegments } from "../lib/donut";

type Preset = "24h" | "7d" | "month" | "all" | "custom";
const presetLabel: Record<Preset, string> = {
  "24h": "Last 24 hours",
  "7d": "Last 7 days",
  month: "This month",
  all: "All footage",
  custom: "Custom range",
};

const CAM_COLORS = ["#3b9ef0", "#2dd4bf", "#f5b23d", "#a78bfa", "#f472b6", "#4ade80", "#fb923c"];
const APP_COLOR = "#6b7280";

function fmtCapacity(sec: number): string {
  if (sec >= 86400 * 2) return `${(sec / 86400).toFixed(1)} days`;
  if (sec >= 86400) return `${(sec / 86400).toFixed(1)} day`;
  if (sec >= 3600) return `${Math.round(sec / 3600)} hours`;
  return `${Math.max(1, Math.round(sec / 60))} min`;
}
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
  size = 180,
  thickness = 22,
}: {
  segments: { value: number; color: string }[];
  centerLabel: string;
  centerSub: string;
  size?: number;
  thickness?: number;
}) {
  const r = (size - thickness) / 2;
  const paths = donutSegments(segments.map((s) => s.value), size / 2, size / 2, r);
  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} className="shrink-0">
      <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="#1a1e27" strokeWidth={thickness} />
      {segments.map((s, i) =>
        paths[i] ? <path key={i} d={paths[i]} fill="none" stroke={s.color} strokeWidth={thickness} strokeLinecap="butt" /> : null,
      )}
      <text x="50%" y="47%" textAnchor="middle" className="fill-fg" style={{ fontSize: 22, fontWeight: 600 }}>
        {centerLabel}
      </text>
      <text x="50%" y="60%" textAnchor="middle" className="fill-fg-faint" style={{ fontSize: 11 }}>
        {centerSub}
      </text>
    </svg>
  );
}

function Stat({ label, value, accent }: { label: string; value: string; accent?: string }) {
  return (
    <div className="rounded-lg border border-line bg-ink-900/40 px-3 py-2">
      <p className="text-[11px] text-fg-faint">{label}</p>
      <p className={`text-sm font-semibold tabular-nums ${accent ? "" : "text-fg"}`} style={accent ? { color: accent } : undefined}>
        {value}
      </p>
    </div>
  );
}

function locIcon(type: string) {
  if (type === "smb") return Server;
  if (type === "sftp") return Cloud;
  return HardDrive;
}

// One camera's row on a destination card: member or not, primary or mirror.
export interface CardCam {
  id: string;
  name: string;
  member: boolean;
  primary: boolean;
  retentionDays: number; // per-location override (0 = the camera's own)
}

function DestinationCard({
  loc,
  cams,
  onEdit,
  onRemove,
  onToggle,
  busy,
}: {
  loc: StorageLocation;
  cams: CardCam[];
  onEdit: () => void;
  onRemove: () => void;
  onToggle: (cameraId: string) => void;
  busy: boolean;
}) {
  // Which cameras record or mirror here is edited ON the card. It used to be
  // reachable only from Settings → Cameras → edit a camera, so clicking through
  // from a card that listed camera names led to the location editor — a dead end.
  const [picking, setPicking] = useState(false);
  const Icon = locIcon(loc.type);
  const path = (loc.config?.path as string) || "";
  const host = (loc.config?.host as string) || "";
  const where =
    loc.type === "local" ? path : loc.type === "smb" ? `\\\\${host}\\${loc.config?.share ?? ""}` : `${loc.config?.user ?? ""}@${host}`;
  const cap = loc.allocationBytes;
  const denom = cap > 0 ? cap : loc.diskTotalBytes || Math.max(1, loc.usedBytes);
  const pct = Math.min(100, Math.round((loc.usedBytes / denom) * 100));
  const capLabel = loc.isDefault ? "hard-reserved" : loc.type === "local" ? "hard-reserved" : "soft cap";

  return (
    <div className={`rounded-xl border p-3.5 ${loc.enabled ? "border-line bg-ink-850" : "border-line/60 bg-ink-850/50"}`}>
      <div className="flex items-start gap-3">
        <div className="grid h-10 w-10 shrink-0 place-items-center rounded-lg bg-brand/12">
          <Icon className="h-5 w-5 text-brand" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="truncate text-sm font-semibold text-fg">{loc.name}</span>
            {loc.isDefault && (
              <span className="rounded bg-brand/15 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-brand">Default</span>
            )}
            <span className="rounded bg-ink-900 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-fg-faint">{loc.type}</span>
            {loc.hasCredential && <Lock className="h-3 w-3 text-fg-faint" />}
            {!loc.enabled && (
              <span className="inline-flex items-center gap-1 text-[10px] text-amber">
                <PauseCircle className="h-3 w-3" /> paused
              </span>
            )}
          </div>
          <p className="truncate font-mono text-[11px] text-fg-faint">{where || "—"}</p>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <button className="btn-icon text-fg-faint hover:text-fg" title="Edit" onClick={onEdit}>
            <Pencil className="h-4 w-4" />
          </button>
          {!loc.isDefault && (
            <button className="btn-icon text-fg-faint hover:text-rec" title="Remove" onClick={onRemove}>
              <Trash2 className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>

      <div className="mt-2.5 h-2 w-full overflow-hidden rounded-full bg-ink-900">
        <div className="h-full rounded-full bg-brand" style={{ width: `${Math.max(pct, loc.usedBytes > 0 ? 2 : 0)}%` }} />
      </div>
      <div className="mt-1.5 flex flex-wrap items-center justify-between gap-x-3 text-[11px] text-fg-faint tabular-nums">
        <span>
          <span className="text-fg-muted">{formatBytes(loc.usedBytes)}</span>
          {cap > 0 ? ` of ${formatBytes(cap)} ${capLabel}` : " used · no limit"}
        </span>
        {loc.diskFreeBytes != null && loc.diskFreeBytes > 0 && <span>{formatBytes(loc.diskFreeBytes)} free on {loc.type === "local" ? "disk" : "share"}</span>}
      </div>
      <div className="mt-2 border-t border-line/60 pt-2">
        <button
          className="flex w-full items-center gap-1.5 text-left text-[11px] text-fg-muted hover:text-fg"
          onClick={() => setPicking((v) => !v)}
          disabled={busy}
        >
          <Video className="h-3 w-3 shrink-0" />
          <span className="truncate">
            {(() => {
              const members = cams.filter((c) => c.member);
              if (members.length === 0) return "No cameras — click to assign";
              return `${members.length} ${members.length === 1 ? "camera" : "cameras"}: ${members
                .map((c) => c.name)
                .join(", ")}`;
            })()}
          </span>
          <ChevronDown className={`ml-auto h-3 w-3 shrink-0 transition-transform ${picking ? "rotate-180" : ""}`} />
        </button>

        {picking && (
          <div className="mt-2 space-y-1 rounded-lg border border-line bg-ink-900 p-2">
            <p className="px-0.5 pb-1 text-[10px] text-fg-faint">
              Tick the cameras that record here. A camera ticked on several destinations records
              to all of them at the same time.
            </p>
            {cams.map((c) => (
              <label key={c.id} className="flex cursor-pointer items-center gap-2 rounded px-0.5 py-1 text-xs text-fg">
                <input
                  type="checkbox"
                  className="accent-brand"
                  checked={c.member}
                  disabled={busy}
                  onChange={() => onToggle(c.id)}
                />
                <span className="truncate">{c.name}</span>
                {c.member && c.retentionDays > 0 && (
                  <span className="ml-auto text-[10px] text-fg-faint">{c.retentionDays}d here</span>
                )}
              </label>
            ))}
            <p className="px-0.5 pt-1 text-[10px] text-fg-faint">
              New footage only — existing recordings stay where they are.
            </p>
          </div>
        )}
      </div>
    </div>
  );
}

export function StorageManager() {
  const { data: storage } = useStorage();
  const { data: locData } = useStorageLocations();
  const { data: cameras } = useCameras();
  const purge = usePurge();
  const reindex = useReindex();
  const del = useDeleteLocation();

  const [cameraId, setCameraId] = useState("");
  const [preset, setPreset] = useState<Preset>("24h");
  const [customFrom, setCustomFrom] = useState(() => toLocalInput(Date.now() - 24 * 3600_000));
  const [customTo, setCustomTo] = useState(() => toLocalInput(Date.now()));
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [result, setResult] = useState<PurgeResult | null>(null);
  const [reindexMsg, setReindexMsg] = useState("");

  const [wizardOpen, setWizardOpen] = useState(false);
  const [editLoc, setEditLoc] = useState<StorageLocation | null>(null);
  // A destination being RECONFIGURED (connection/credentials) through the wizard's
  // probe-gated flow — base setup is only changeable behind a passing test.
  const [wizardEdit, setWizardEdit] = useState<StorageLocation | null>(null);
  const [delLoc, setDelLoc] = useState<StorageLocation | null>(null);
  const [forceLoc, setForceLoc] = useState<StorageLocation | null>(null);

  const locations = locData?.locations ?? storage?.locations ?? [];

  // Globals across all destinations.
  const recorded = storage?.recordedBytes ?? 0;
  const app = storage?.appBytes ?? 0;
  const totalAlloc = locations.reduce((s, l) => s + Math.max(0, l.allocationBytes), 0);
  const totalUsed = locations.reduce((s, l) => s + l.usedBytes, 0);
  const freeInAlloc = Math.max(0, totalAlloc - totalUsed);

  // Which cameras record to each location (by id).
  const setDest = useSetCameraDestinations();

  // Memberships come from the locations themselves (each lists its cameras with a
  // primary flag), so mirrors show up — deriving from cameras.locationId would
  // only ever show the primary.
  const membershipsOf = (camId: string) => {
    const out: { locationId: string; retentionDays: number; primary: boolean }[] = [];
    for (const l of locations) {
      const m = (l.cameras ?? []).find((c) => c.cameraId === camId);
      if (m) out.push({ locationId: l.id, retentionDays: m.retentionDays, primary: m.primary });
    }
    return out;
  };
  const cardCams = (locId: string): CardCam[] =>
    (cameras ?? []).map((c) => {
      const m = (locations.find((l) => l.id === locId)?.cameras ?? []).find((x) => x.cameraId === c.id);
      return {
        id: c.id,
        name: c.name,
        member: !!m,
        primary: m?.primary ?? false,
        retentionDays: m?.retentionDays ?? 0,
      };
    });
  // toggleCamera: tick/untick a camera on a destination. Ticked on several =
  // records to all of them. The write target (internal "primary") follows
  // automatically: it only moves when its own destination is unticked, and a
  // camera left with nothing falls back to Primary storage.
  const toggleCamera = (camId: string, locId: string) => {
    const ms = membershipsOf(camId);
    const member = ms.some((m) => m.locationId === locId);
    let next = member
      ? ms.filter((m) => m.locationId !== locId)
      : [...ms, { locationId: locId, retentionDays: 0, primary: false }];
    if (next.length === 0) next = [{ locationId: "", retentionDays: 0, primary: true }];
    const prevPrimary = ms.find((m) => m.primary)?.locationId;
    const primary =
      prevPrimary !== undefined && next.some((m) => m.locationId === prevPrimary)
        ? prevPrimary
        : next.some((m) => m.locationId === "")
          ? ""
          : next[0].locationId;
    setDest.mutate({
      cameraId: camId,
      primaryLocationId: primary,
      destinations: next.map((m) => ({ locationId: m.locationId, retentionDays: m.retentionDays })),
    });
  };


  // Per-camera footage donut (footage by camera + app data = total footprint).
  const camSegs = (storage?.cameras ?? []).map((c, i) => ({ value: c.bytes, color: CAM_COLORS[i % CAM_COLORS.length] }));
  const donutSegs = [...camSegs, { value: app, color: APP_COLOR }];

  function range(): { from: number; to: number } {
    const now = Date.now();
    switch (preset) {
      case "24h": return { from: now - 24 * 3600_000, to: now };
      case "7d": return { from: now - 7 * 24 * 3600_000, to: now };
      case "month": return { from: startOfMonth(), to: now };
      case "all": return { from: 0, to: now };
      case "custom": return { from: new Date(customFrom).getTime(), to: new Date(customTo).getTime() };
    }
  }
  const r = range();
  const validRange = r.to > r.from;
  const camLabel = cameraId ? cameras?.find((c) => c.id === cameraId)?.name : "all cameras";

  async function doReindex() {
    setReindexMsg("");
    try {
      const res = await reindex.mutateAsync();
      setReindexMsg(res.recovered > 0 ? `Recovered ${res.recovered.toLocaleString()} orphaned segments.` : "Everything on disk is already indexed.");
    } catch {
      setReindexMsg("Re-scan failed.");
    }
  }
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
  async function removeLoc(force: boolean) {
    const loc = force ? forceLoc : delLoc;
    if (!loc) return;
    try {
      await del.mutateAsync({ id: loc.id, force });
      setDelLoc(null);
      setForceLoc(null);
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        setDelLoc(null);
        setForceLoc(loc);
      } else {
        setDelLoc(null);
        setForceLoc(null);
      }
    }
  }

  return (
    <div className="panel space-y-5">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-medium text-fg">Storage</h2>
        <button className="btn-subtle h-8" disabled={reindex.isPending} onClick={doReindex} title="Index any footage on disk the database lost track of">
          <RefreshCw className={`h-3.5 w-3.5 ${reindex.isPending ? "animate-spin" : ""}`} />
          {reindex.isPending ? "Scanning…" : "Re-scan"}
        </button>
      </div>
      {reindexMsg && <p className="-mt-2 text-xs text-online">{reindexMsg}</p>}

      {/* Global overview */}
      {storage && (
        <div className="flex flex-col items-center gap-5 rounded-xl border border-line bg-ink-850 p-4 sm:flex-row">
          <Donut
            segments={donutSegs}
            centerLabel={formatBytes(recorded + app)}
            centerSub="stored"
          />
          <div className="w-full flex-1">
            <div className="grid grid-cols-2 gap-2">
              <Stat label="Recorded footage" value={formatBytes(recorded)} />
              <Stat label="App & database" value={formatBytes(app)} />
              <Stat label="Allocated to Aperture" value={totalAlloc > 0 ? formatBytes(totalAlloc) : "—"} accent="#3b9ef0" />
              <Stat label="Free in allocation" value={totalAlloc > 0 ? formatBytes(freeInAlloc) : "—"} />
            </div>
            <p className="mt-2.5 text-[11px] text-fg-faint">
              Across {locations.length} {locations.length === 1 ? "destination" : "destinations"} · {cameras?.length ?? 0}{" "}
              {(cameras?.length ?? 0) === 1 ? "camera" : "cameras"}
            </p>
            {storage.recordingRateBps > 0 && totalAlloc > 0 && (
              <div className="mt-2 flex items-center gap-2 text-[11px] text-fg-muted">
                <Timer className="h-3.5 w-3.5 shrink-0 text-brand" />
                Holds ≈ <span className="font-medium text-fg">{fmtCapacity((freeInAlloc / storage.recordingRateBps) || 0)}</span> more at the current rate
              </div>
            )}
          </div>
        </div>
      )}

      {/* Destinations */}
      <div>
        <div className="mb-2.5 flex items-center justify-between">
          <div>
            <p className="text-xs font-medium text-fg-muted">Recording destinations</p>
            <p className="text-[11px] text-fg-faint">Local disks and network shares. Point cameras at them from each camera’s settings.</p>
          </div>
          <button className="btn-subtle h-8 shrink-0" onClick={() => setWizardOpen(true)}>
            <Plus className="h-3.5 w-3.5" />
            Add
          </button>
        </div>
        <div className="space-y-2.5">
          {locations.map((loc) => (
            <DestinationCard
              key={loc.id || "default"}
              loc={loc}
              cams={cardCams(loc.id)}
              onEdit={() => setEditLoc(loc)}
              onRemove={() => setDelLoc(loc)}
              busy={setDest.isPending}
              onToggle={(camId) => toggleCamera(camId, loc.id)}
            />
          ))}
        </div>
        {locData?.dataDir && (
          <p className="mt-2.5 flex items-center gap-1.5 text-[11px] text-fg-faint">
            <Lock className="h-3 w-3" />
            App &amp; database stay at <span className="font-mono">{locData.dataDir}</span> (fixed with the install).
          </p>
        )}
      </div>

      {/* Delete footage */}
      <div className="border-t border-line pt-4">
        <p className="mb-2.5 text-xs font-medium text-fg-muted">Delete footage</p>
        <div className="mb-3 grid grid-cols-2 gap-3">
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
          <div className="mb-3 grid grid-cols-2 gap-3">
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
      <ConfirmDialog
        open={!!delLoc}
        danger
        busy={del.isPending}
        title="Remove destination"
        message={`Remove “${delLoc?.name}” as a recording destination? Its cameras must be reassigned and its footage removed first.`}
        confirmLabel="Remove"
        onConfirm={() => removeLoc(false)}
        onCancel={() => setDelLoc(null)}
      />
      <ConfirmDialog
        open={!!forceLoc}
        danger
        busy={del.isPending}
        title="Force-remove destination"
        message={`“${forceLoc?.name}” still has cameras or footage. Force-removing reassigns its cameras back to the primary storage and permanently deletes the footage stored here. This cannot be undone.`}
        confirmLabel="Force remove"
        onConfirm={() => removeLoc(true)}
        onCancel={() => setForceLoc(null)}
      />

      <StorageSetupWizard
        open={wizardOpen || !!wizardEdit}
        onClose={() => {
          setWizardOpen(false);
          setWizardEdit(null);
        }}
        initialStep="type"
        edit={wizardEdit}
      />
      <EditLocationModal
        loc={editLoc}
        onClose={() => setEditLoc(null)}
        onReconfigure={() => {
          setWizardEdit(editLoc);
          setEditLoc(null);
        }}
      />
    </div>
  );
}
