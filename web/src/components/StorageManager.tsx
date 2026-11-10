import { useState, type ReactNode } from "react";
import clsx from "clsx";
import { Trash2, RefreshCw, Plus, HardDrive, Server, Cloud, Lock, Pencil, Timer, PauseCircle, Video, ChevronDown } from "lucide-react";
import { ConfirmDialog } from "./ui/ConfirmDialog";
import { SettingsHeading, SettingsRow } from "./ui/Layout";
import { Skeleton } from "./ui/Skeleton";
import { StorageSetupWizard } from "./storage/StorageSetupWizard";
import { EditLocationModal } from "./storage/EditLocationModal";
import { useStorage, type StorageLocation } from "../hooks/useStorage";
import { useStorageLocations, useDeleteLocation, useSetCameraDestinations } from "../hooks/useStorageLocations";
import { useCameras } from "../hooks/useCameras";
import { usePurge, useReindex, type PurgeResult } from "../hooks/useSystem";
import { ApiError } from "../lib/api";
import { formatBytes } from "../lib/format";

type Preset = "24h" | "7d" | "month" | "all" | "custom";
const presetLabel: Record<Preset, string> = {
  "24h": "Last 24 hours",
  "7d": "Last 7 days",
  month: "This month",
  all: "All footage",
  custom: "Custom range",
};

// Categorical fills for the per-camera segments of the usage bar (and their
// legend swatches). App data is the light grey block at the bar's far end; the
// bare track is the free space left in the allocation.
const CAM_FILLS = ["bg-violet-400", "bg-brand", "bg-online", "bg-amber-400", "bg-teal-400", "bg-pink-400", "bg-sky-300"];
const APP_FILL = "bg-fg-muted";
const TRACK = "bg-ink-600";

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

// UsageBar is the single horizontal stacked bar of the overview: footage per
// camera from the left, app data pinned to the right end, free space between.
function UsageBar({ segments, total }: { segments: { value: number; fill: string; right?: boolean }[]; total: number }) {
  const pct = (v: number) => (total > 0 ? Math.max(0, (v / total) * 100) : 0);
  const left = segments.filter((s) => !s.right && s.value > 0);
  const right = segments.filter((s) => s.right && s.value > 0);
  return (
    <div className={clsx("flex h-2 w-full overflow-hidden rounded-full", TRACK)}>
      {left.map((s, i) => (
        <div key={i} className={clsx("h-full shrink-0 border-r border-ink-900 last:border-r-0", s.fill)} style={{ width: `${Math.max(pct(s.value), 0.6)}%` }} />
      ))}
      <div className="flex-1" />
      {right.map((s, i) => (
        <div key={i} className={clsx("h-full shrink-0 border-l border-ink-900", s.fill)} style={{ width: `${Math.max(pct(s.value), 0.6)}%` }} />
      ))}
    </div>
  );
}

function LegendItem({ fill, label, value }: { fill: string; label: string; value: string }) {
  return (
    <span className="inline-flex items-center gap-1.5 whitespace-nowrap text-xs text-fg-muted">
      <span className={clsx("h-2 w-2 shrink-0 rounded-[2px]", fill)} />
      {label}
      <span className="tabular-nums text-fg-faint">{value}</span>
    </span>
  );
}

// A list's heading with its create action on the right.
function ListHeader({ title, description, action }: { title: string; description?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2 pt-6 first:pt-0">
      <div className="min-w-0">
        <h3 className="text-sm font-semibold text-fg">{title}</h3>
        {description && <p className="mt-1 text-xs leading-relaxed text-fg-faint">{description}</p>}
      </div>
      {action}
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

function DestinationRow({
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
  // Which cameras record or mirror here is edited ON the row. It used to be
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
  const members = cams.filter((c) => c.member);

  return (
    <div
      className={clsx(
        "grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-6 gap-y-2.5 py-3.5",
        "md:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)_minmax(0,1fr)_72px]",
        !loc.enabled && "opacity-70",
      )}
    >
      {/* Name, type and where it points */}
      <div className="flex min-w-0 items-center gap-3">
        <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-ink-800">
          <Icon className="h-[18px] w-[18px] text-fg-muted" />
        </div>
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="truncate text-sm font-medium text-fg">{loc.name}</span>
            {loc.isDefault && <span className="pill-blue">Default</span>}
            <span className="pill-gray uppercase">{loc.type}</span>
            {loc.hasCredential && <Lock className="h-3 w-3 text-fg-faint" aria-label="Credentials stored" />}
            {!loc.enabled && (
              <span className="pill-orange">
                <PauseCircle className="h-3 w-3" /> paused
              </span>
            )}
          </div>
          <p className="mt-0.5 truncate font-mono text-[11px] text-fg-faint">{where || "—"}</p>
        </div>
      </div>

      {/* Actions (top-right on a phone, last column on desktop) */}
      <div className="flex justify-end md:order-last">
        <button className="btn-icon" title="Edit" aria-label="Edit" onClick={onEdit}>
          <Pencil className="h-4 w-4" />
        </button>
        {!loc.isDefault && (
          <button className="btn-icon hover:text-danger" title="Remove" aria-label="Remove" onClick={onRemove}>
            <Trash2 className="h-4 w-4" />
          </button>
        )}
      </div>

      {/* Usage */}
      <div className="col-span-2 min-w-0 md:col-span-1">
        <div className="h-1.5 w-full overflow-hidden rounded-full bg-ink-650">
          <div className="h-full rounded-full bg-brand" style={{ width: `${Math.max(pct, loc.usedBytes > 0 ? 2 : 0)}%` }} />
        </div>
        <div className="mt-1.5 flex flex-wrap items-center justify-between gap-x-3 text-[11px] tabular-nums text-fg-faint">
          <span>
            <span className="text-fg-muted">{formatBytes(loc.usedBytes)}</span>
            {cap > 0 ? ` of ${formatBytes(cap)} ${capLabel}` : " used · no limit"}
          </span>
          {loc.diskFreeBytes != null && loc.diskFreeBytes > 0 && (
            <span>
              {formatBytes(loc.diskFreeBytes)} free on {loc.type === "local" ? "disk" : "share"}
            </span>
          )}
        </div>
      </div>

      {/* Cameras recording here — opens the picker below the row */}
      <div className="col-span-2 min-w-0 md:col-span-1">
        <button
          className="flex w-full min-w-0 items-center gap-2 rounded-md text-left text-sm text-fg-muted transition-colors hover:text-fg"
          onClick={() => setPicking((v) => !v)}
          disabled={busy}
          aria-expanded={picking}
        >
          <Video className="h-4 w-4 shrink-0 text-fg-faint" />
          <span className="min-w-0 truncate">
            {members.length === 0 ? (
              <span className="text-brand">No cameras — click to assign</span>
            ) : (
              <>
                <span className="text-fg">
                  {members.length} {members.length === 1 ? "camera" : "cameras"}
                </span>
                <span className="text-fg-faint"> · {members.map((c) => c.name).join(", ")}</span>
              </>
            )}
          </span>
          <ChevronDown className={clsx("ml-auto h-4 w-4 shrink-0 text-fg-faint transition-transform", picking && "rotate-180")} />
        </button>
      </div>

      {picking && (
        <div className="col-span-full mb-0.5 rounded-lg border border-line p-3 md:order-last">
          <p className="mb-2 text-xs text-fg-faint">
            Tick the cameras that record here. A camera ticked on several destinations records
            to all of them at the same time.
          </p>
          <div className="grid grid-cols-1 gap-x-6 sm:grid-cols-2 lg:grid-cols-3">
            {cams.map((c) => (
              <label
                key={c.id}
                className="flex h-8 cursor-pointer items-center gap-2.5 rounded-md px-1 text-sm text-fg hover:bg-white/[0.03]"
              >
                <input type="checkbox" checked={c.member} disabled={busy} onChange={() => onToggle(c.id)} />
                <span className="min-w-0 truncate">{c.name}</span>
                {c.member && c.retentionDays > 0 && (
                  <span className="ml-auto shrink-0 text-[11px] text-fg-faint">{c.retentionDays}d here</span>
                )}
              </label>
            ))}
          </div>
          <p className="mt-2 text-[11px] text-fg-faint">New footage only — existing recordings stay where they are.</p>
        </div>
      )}
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


  // Usage bar: footage by camera + app data (= the total footprint), measured
  // against the allocation when there is one.
  // Colours go to cameras that actually hold footage, so the first ones are
  // never spent on empty cameras.
  const camSegs = (storage?.cameras ?? [])
    .filter((c) => c.bytes > 0)
    .map((c, i) => ({ name: c.name, value: c.bytes, fill: CAM_FILLS[i % CAM_FILLS.length] }));
  const barTotal = Math.max(totalAlloc, totalUsed, recorded + app, 1);

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
    <>
      {/* Overview */}
      <section className="panel">
        {storage ? (
          <>
            <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
              <h3 className="text-sm font-semibold text-fg">
                Overview{" "}
                <span className="font-normal tabular-nums text-fg-muted">
                  ({formatBytes(totalUsed || recorded + app)}
                  {totalAlloc > 0 ? ` / ${formatBytes(totalAlloc)}` : " stored"})
                </span>
              </h3>
              {storage.recordingRateBps > 0 && totalAlloc > 0 && (
                <p className="flex items-center gap-1.5 text-xs text-fg-muted">
                  <Timer className="h-3.5 w-3.5 shrink-0 text-fg-faint" />
                  Holds ≈ <span className="font-medium text-fg">{fmtCapacity((freeInAlloc / storage.recordingRateBps) || 0)}</span> more at the current rate
                </p>
              )}
            </div>

            <div className="mt-4">
              <UsageBar
                total={barTotal}
                segments={[...camSegs, { value: app, fill: APP_FILL, right: true }]}
              />
            </div>
            <div className="mt-2.5 flex flex-wrap gap-x-4 gap-y-1.5">
              {camSegs.map((c, i) => (
                <LegendItem key={i} fill={c.fill} label={c.name} value={formatBytes(c.value)} />
              ))}
              <LegendItem fill={APP_FILL} label="App & database" value={formatBytes(app)} />
              {totalAlloc > 0 && <LegendItem fill={TRACK} label="Free in allocation" value={formatBytes(freeInAlloc)} />}
            </div>

            {/* The destinations at a glance, like drive chips. */}
            <div className="mt-4 flex flex-wrap gap-2">
              {locations.map((l) => (
                <span
                  key={l.id || "default"}
                  className="inline-flex h-7 items-center gap-2 rounded-md bg-ink-800 px-2.5 text-xs text-fg"
                >
                  <span className={clsx("h-1.5 w-1.5 rounded-full", l.enabled ? "bg-online" : "bg-warn")} />
                  {l.name}
                  <span className="tabular-nums text-fg-faint">
                    {l.allocationBytes > 0 ? formatBytes(l.allocationBytes) : "no limit"}
                  </span>
                </span>
              ))}
            </div>

            <div className="mt-4 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-fg-faint">
              <span className="tabular-nums">
                Recorded footage {formatBytes(recorded)} · Allocated to Aperture{" "}
                {totalAlloc > 0 ? formatBytes(totalAlloc) : "—"} · Across {locations.length}{" "}
                {locations.length === 1 ? "destination" : "destinations"} · {cameras?.length ?? 0}{" "}
                {(cameras?.length ?? 0) === 1 ? "camera" : "cameras"}
              </span>
            </div>

            <div className="mt-4 flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-line pt-4">
              <button
                className="btn-link"
                disabled={reindex.isPending}
                onClick={doReindex}
                title="Index any footage on disk the database lost track of"
              >
                <RefreshCw className={clsx("h-3.5 w-3.5", reindex.isPending && "animate-spin")} />
                {reindex.isPending ? "Scanning…" : "Re-scan"}
              </button>
              {reindexMsg && <span className="text-xs text-online">{reindexMsg}</span>}
            </div>
          </>
        ) : (
          <div className="space-y-3">
            <Skeleton className="h-4 w-56" />
            <Skeleton className="h-2 w-full" />
            <Skeleton className="h-3 w-80" />
          </div>
        )}
      </section>

      {/* Destinations */}
      <ListHeader
        title="Recording destinations"
        description="Local disks and network shares. Point cameras at them from each camera’s settings."
        action={
          <button className="btn-outline shrink-0" onClick={() => setWizardOpen(true)}>
            <Plus className="h-4 w-4" />
            Add destination
          </button>
        }
      />
      <div>
        <div className="hidden grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)_minmax(0,1fr)_72px] gap-x-6 border-b border-line pb-2 text-[13px] font-semibold text-fg md:grid">
          <span>Name</span>
          <span>Usage</span>
          <span>Cameras</span>
          <span />
        </div>
        <div className="divide-y divide-line border-b border-line">
          {locations.map((loc) => (
            <DestinationRow
              key={loc.id || "default"}
              loc={loc}
              cams={cardCams(loc.id)}
              onEdit={() => setEditLoc(loc)}
              onRemove={() => setDelLoc(loc)}
              busy={setDest.isPending}
              onToggle={(camId) => toggleCamera(camId, loc.id)}
            />
          ))}
          {locations.length === 0 && !storage && (
            <div className="space-y-2 py-3">
              <Skeleton className="h-9" />
              <Skeleton className="h-9" />
            </div>
          )}
        </div>
        {locData?.dataDir && (
          <p className="mt-2.5 text-[11px] leading-relaxed text-fg-faint">
            <Lock className="-mt-0.5 mr-1.5 inline h-3 w-3" />
            App &amp; database stay at <span className="break-all font-mono">{locData.dataDir}</span> (fixed with the
            install).
          </p>
        )}
      </div>

      {/* Delete footage */}
      <SettingsHeading description="Permanently removes recordings in a time range.">Delete footage</SettingsHeading>
      <div className="divide-y divide-line">
        <SettingsRow label="Camera">
          <select className="input max-w-[280px]" value={cameraId} onChange={(e) => setCameraId(e.target.value)} aria-label="Camera">
            <option value="">All cameras</option>
            {cameras?.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        </SettingsRow>
        <SettingsRow label="Range">
          <select className="input max-w-[280px]" value={preset} onChange={(e) => setPreset(e.target.value as Preset)} aria-label="Range">
            {(["24h", "7d", "month", "all", "custom"] as Preset[]).map((p) => (
              <option key={p} value={p}>
                {presetLabel[p]}
              </option>
            ))}
          </select>
        </SettingsRow>
        {preset === "custom" && (
          <SettingsRow label="From – to">
            <div className="grid max-w-lg grid-cols-1 gap-3 sm:grid-cols-2">
              <input
                type="datetime-local"
                className="input"
                value={customFrom}
                onChange={(e) => setCustomFrom(e.target.value)}
                aria-label="From"
              />
              <input
                type="datetime-local"
                className="input"
                value={customTo}
                onChange={(e) => setCustomTo(e.target.value)}
                aria-label="To"
              />
            </div>
          </SettingsRow>
        )}
        <SettingsRow label="">
          <div className="flex flex-wrap items-center gap-3">
            <button
              className="btn-ghost text-danger hover:border-danger/50 hover:bg-danger/10"
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
        </SettingsRow>
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
    </>
  );
}
