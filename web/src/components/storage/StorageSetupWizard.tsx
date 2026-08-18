import { useEffect, useState } from "react";
import {
  HardDrive,
  Server,
  Cloud,
  Database,
  Plus,
  X,
  ChevronLeft,
  ChevronRight,
  Check,
  Loader2,
  ShieldCheck,
  Lock,
  Video,
  Clock,
  AlertTriangle,
  Trash2,
} from "lucide-react";
import { useScrollLock } from "../../hooks/useScrollLock";
import { useStorage } from "../../hooks/useStorage";
import type { StorageLocation } from "../../hooks/useStorage";
import {
  useStorageLocations,
  useCreateLocation,
  useAssignCameras,
  useDeleteLocation,
  useTestLocation,
  useUpdateLocation,
  useAllocateLocation,
  usePreviewAllocation,
  type EvictionPreview,
  type TestStep,
} from "../../hooks/useStorageLocations";
import { EvictionConfirm } from "./EvictionConfirm";
import { useCameras } from "../../hooks/useCameras";
import { StorageWizard } from "../StorageWizard";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { ApiError } from "../../lib/api";
import { formatBytes } from "../../lib/format";

const GiB = 1024 ** 3;
const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));

function fmtDuration(sec: number): string {
  if (!isFinite(sec) || sec <= 0) return "—";
  if (sec >= 86400 * 2) return `${(sec / 86400).toFixed(1)} days`;
  if (sec >= 86400) return "1 day";
  if (sec >= 3600) return `${Math.round(sec / 3600)} hours`;
  return `${Math.max(1, Math.round(sec / 60))} min`;
}

// A human, secret-free one-liner describing where a draft points.
function connectionSummary(d: Draft): string {
  const c = d.config;
  if (d.type === "local") return String(c.path ?? "");
  if (d.type === "smb") return `\\\\${c.host ?? ""}\\${c.share ?? ""}${c.subfolder ? "\\" + c.subfolder : ""}`;
  return `${c.user ?? ""}@${c.host ?? ""}:${c.path ?? ""}`;
}

type Step = "overview" | "type" | "details" | "testing" | "allocate" | "done";
type LocType = "local" | "smb" | "sftp";

interface Draft {
  type: LocType;
  name: string;
  config: Record<string, string | number>;
  secret: string;
  allocationBytes: number;
  cameraIds: string[];
  diskFreeBytes: number;
  diskTotalBytes: number;
  // Edit mode: reconfiguring an EXISTING destination through the same probe-gated
  // flow (empty secret = keep the stored credential; the probe uses it via
  // locationId). Base setup and credentials are ONLY changeable through here.
  editId?: string;
  editEtag?: string;
  editAllocation?: number; // current allocation, to detect a decrease
}

const emptyDraft: Draft = {
  type: "local",
  name: "",
  config: {},
  secret: "",
  allocationBytes: 0,
  cameraIds: [],
  diskFreeBytes: 0,
  diskTotalBytes: 0,
};

export function StorageSetupWizard({
  open,
  onClose,
  initialStep = "overview",
  edit = null,
}: {
  open: boolean;
  onClose: () => void;
  initialStep?: Step;
  // A network destination to reconfigure: the wizard prefills from it, probes the
  // (possibly unchanged) connection, and APPLIES as an update instead of a create.
  edit?: StorageLocation | null;
}) {
  const { data } = useStorageLocations();
  const { data: cameras } = useCameras();
  const { data: storage } = useStorage();
  const create = useCreateLocation();
  const assign = useAssignCameras();
  const update = useUpdateLocation();
  const allocate = useAllocateLocation();
  const previewAlloc = usePreviewAllocation();
  const [pendingEvict, setPendingEvict] = useState<EvictionPreview | null>(null);
  const [step, setStep] = useState<Step>(initialStep);
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [reserveOpen, setReserveOpen] = useState(false);
  const [saveErr, setSaveErr] = useState("");
  useScrollLock(open);

  useEffect(() => {
    if (open) {
      if (edit) {
        setDraft({
          ...emptyDraft,
          type: edit.type,
          name: edit.name,
          config: { ...(edit.config as Record<string, string | number>) },
          allocationBytes: edit.allocationBytes,
          editId: edit.id,
          editEtag: edit.etag,
          editAllocation: edit.allocationBytes,
        });
        setStep("details");
      } else {
        setStep(initialStep);
      }
    } else {
      setDraft(emptyDraft);
      setSaveErr("");
      setPendingEvict(null);
    }
  }, [open, initialStep, edit]);

  const locations = data?.locations ?? [];
  // Average per-camera byte rate, to estimate how long an allocation holds.
  const perCamBps =
    storage && storage.recordingCameras > 0 ? storage.recordingRateBps / storage.recordingCameras : 0;

  function startAdd() {
    setDraft(emptyDraft);
    setSaveErr("");
    setStep("type");
  }

  async function finishSave() {
    setSaveErr("");
    try {
      if (draft.editId) {
        // Shrinking the allocation destroys footage immediately — confirm with the
        // server-computed impact first, exactly like the edit modal does.
        if (draft.allocationBytes < (draft.editAllocation ?? 0)) {
          const p = await previewAlloc.mutateAsync({
            locationId: draft.editId,
            allocationBytes: draft.allocationBytes,
          });
          if (p.willEvict) {
            setPendingEvict(p);
            return;
          }
        }
        await applyEdit();
        return;
      }
      const loc = await create.mutateAsync({
        name: draft.name.trim(),
        type: draft.type,
        config: draft.config,
        secret: draft.secret || undefined,
        allocationBytes: draft.allocationBytes,
      });
      if (draft.cameraIds.length > 0) {
        await assign.mutateAsync({ locationId: loc.id, cameraIds: draft.cameraIds });
      }
      setStep("done");
    } catch (e) {
      setSaveErr(e instanceof Error ? e.message : "Could not save the destination.");
    }
  }

  async function applyEdit() {
    setPendingEvict(null);
    try {
      await update.mutateAsync({
        id: draft.editId!,
        etag: draft.editEtag,
        name: draft.name.trim() || undefined,
        config: draft.config,
        secret: draft.secret || undefined, // empty = keep the stored credential
      });
      await allocate.mutateAsync({ locationId: draft.editId!, allocationBytes: draft.allocationBytes });
      setStep("done");
    } catch (e) {
      setSaveErr(e instanceof Error ? e.message : "Could not apply the changes.");
    }
  }

  if (!open) return null;

  const stepIndex = ["overview", "type", "details", "testing", "allocate", "done"].indexOf(step);

  return (
    <div className="fixed inset-0 z-50 grid place-items-center p-4" style={{ zIndex: 60 }}>
      <div className="absolute inset-0 bg-black/70 animate-fade-in" onClick={onClose} />
      <div className="relative w-[min(96vw,680px)] card overflow-hidden shadow-pop animate-fade-in">
        {/* header */}
        <div className="flex items-center gap-3 px-5 pt-4 pb-3 border-b border-line">
          <Database className="h-4 w-4 text-brand" />
          <span className="text-sm font-medium text-fg">Storage setup</span>
          <div className="ml-auto flex items-center gap-1.5">
            {["overview", "type", "details", "testing", "allocate", "done"].map((s, i) => (
              <span
                key={s}
                className={`h-1.5 rounded-full transition-all ${
                  i === stepIndex ? "w-6 bg-brand" : i < stepIndex ? "w-4 bg-brand/50" : "w-4 bg-ink-700"
                }`}
              />
            ))}
            <button onClick={onClose} className="ml-2 btn-icon h-7 w-7" aria-label="Close">
              <X className="h-4 w-4" />
            </button>
          </div>
        </div>

        <div className="max-h-[75vh] overflow-y-auto">
          {step === "overview" && (
            <OverviewStep
              locations={locations}
              dataDir={data?.dataDir}
              onAdd={startAdd}
              onAdjustPrimary={() => setReserveOpen(true)}
              onClose={onClose}
            />
          )}
          {step === "type" && (
            <TypeStep
              value={draft.type}
              onPick={(t) => setDraft((d) => ({ ...d, type: t }))}
              onBack={() => (initialStep === "type" ? onClose() : setStep("overview"))}
              onNext={() => setStep("details")}
            />
          )}
          {step === "details" && (
            <DetailsStep
              draft={draft}
              setDraft={setDraft}
              onBack={() => (draft.editId ? onClose() : setStep("type"))}
              onNext={() => setStep("testing")}
            />
          )}
          {step === "testing" && (
            <TestingStep
              draft={draft}
              onBack={() => setStep("details")}
              onPass={(free, total, hostKey) => {
                setDraft((d) => ({
                  ...d,
                  diskFreeBytes: free,
                  diskTotalBytes: total,
                  // Pin the SFTP host key observed during the probe so future
                  // connections verify the server's identity (TOFU).
                  config: hostKey ? { ...d.config, hostKey } : d.config,
                  // Seed from free space when known; otherwise a sensible soft cap
                  // (network shares that don't report capacity, e.g. some SFTP).
                  allocationBytes: d.allocationBytes || (free > 0 ? Math.round(free * 0.6) : 500 * GiB),
                }));
                setStep("allocate");
              }}
            />
          )}
          {step === "allocate" && (
            <AllocateStep
              draft={draft}
              setDraft={setDraft}
              cameras={cameras ?? []}
              locations={locations}
              perCamBps={perCamBps}
              saving={create.isPending || assign.isPending || update.isPending || allocate.isPending || previewAlloc.isPending}
              error={saveErr}
              onBack={() => setStep("testing")}
              onSave={finishSave}
            />
          )}
          {step === "done" && (
            <DoneStep draft={draft} onAddAnother={startAdd} onClose={onClose} />
          )}
        </div>
      </div>

      <StorageWizard open={reserveOpen} onClose={() => setReserveOpen(false)} />
      <EvictionConfirm
        preview={pendingEvict}
        busy={update.isPending || allocate.isPending}
        onCancel={() => setPendingEvict(null)}
        onConfirm={applyEdit}
      />
    </div>
  );
}

/* ---------- Step 1: Overview ---------- */

function OverviewStep({
  locations,
  dataDir,
  onAdd,
  onAdjustPrimary,
  onClose,
}: {
  locations: StorageLocation[];
  dataDir?: string;
  onAdd: () => void;
  onAdjustPrimary: () => void;
  onClose: () => void;
}) {
  const del = useDeleteLocation();
  const [toDelete, setToDelete] = useState<StorageLocation | null>(null);
  const [forceLoc, setForceLoc] = useState<StorageLocation | null>(null);

  async function remove(force: boolean) {
    const loc = force ? forceLoc : toDelete;
    if (!loc) return;
    try {
      await del.mutateAsync({ id: loc.id, force });
      setToDelete(null);
      setForceLoc(null);
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        setToDelete(null);
        setForceLoc(loc);
      } else {
        setToDelete(null);
        setForceLoc(null);
      }
    }
  }

  return (
    <div className="p-5">
      <h3 className="text-base font-semibold text-fg">Where Aperture keeps recordings</h3>
      <p className="mt-1 text-sm text-fg-muted">
        Add disks or network shares as recording destinations, then point cameras at them. Footage already recorded
        stays where it is.
      </p>

      {/* Fixed app & database */}
      {dataDir && (
        <div className="mt-4 flex items-center gap-3 rounded-lg border border-line bg-ink-850 p-3">
          <div className="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-ink-900">
            <Lock className="h-4 w-4 text-fg-faint" />
          </div>
          <div className="min-w-0 flex-1">
            <p className="text-sm text-fg">App &amp; database</p>
            <p className="truncate font-mono text-[11px] text-fg-faint">{dataDir}</p>
          </div>
          <span className="shrink-0 rounded bg-ink-900 px-2 py-0.5 text-[10px] uppercase tracking-wide text-fg-faint">
            Fixed
          </span>
        </div>
      )}

      {/* Destinations */}
      <p className="mt-5 mb-2 text-xs font-medium text-fg-muted">Recording destinations</p>
      <div className="space-y-2.5">
        {locations.map((loc) => (
          <DestinationCard
            key={loc.id || "default"}
            loc={loc}
            onAdjustPrimary={onAdjustPrimary}
            onDelete={() => setToDelete(loc)}
          />
        ))}
      </div>

      <button className="btn-subtle w-full mt-3 h-10 border-dashed" onClick={onAdd}>
        <Plus className="h-4 w-4" />
        Add a destination
      </button>

      <div className="flex justify-end mt-5">
        <button className="btn-primary h-9" onClick={onClose}>
          Done
        </button>
      </div>

      <ConfirmDialog
        open={!!toDelete}
        danger
        busy={del.isPending}
        title="Remove destination"
        message={`Remove “${toDelete?.name}” as a recording destination? Its cameras must be reassigned and its footage removed first.`}
        confirmLabel="Remove"
        onConfirm={() => remove(false)}
        onCancel={() => setToDelete(null)}
      />
      <ConfirmDialog
        open={!!forceLoc}
        danger
        busy={del.isPending}
        title="Force-remove destination"
        message={`“${forceLoc?.name}” still has cameras or footage. Force-removing reassigns its cameras back to the primary storage and permanently deletes the footage stored here. This cannot be undone.`}
        confirmLabel="Force remove"
        onConfirm={() => remove(true)}
        onCancel={() => setForceLoc(null)}
      />
    </div>
  );
}

function DestinationCard({
  loc,
  onAdjustPrimary,
  onDelete,
}: {
  loc: StorageLocation;
  onAdjustPrimary: () => void;
  onDelete: () => void;
}) {
  const path = (loc.config?.path as string) || "";
  const cap = loc.allocationBytes;
  const denom = cap > 0 ? cap : loc.diskTotalBytes || Math.max(1, loc.usedBytes);
  const pct = Math.min(100, Math.round((loc.usedBytes / denom) * 100));
  const isNet = loc.type === "smb" || loc.type === "sftp";
  return (
    <div className="rounded-lg border border-line bg-ink-850 p-3">
      <div className="flex items-start gap-3">
        <div className="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-brand/12">
          {isNet ? <Server className="h-5 w-5 text-brand" /> : <HardDrive className="h-5 w-5 text-brand" />}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="truncate text-sm font-medium text-fg">{loc.name}</span>
            {loc.isDefault && (
              <span className="rounded bg-brand/15 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-brand">
                Primary
              </span>
            )}
          </div>
          <p className="truncate font-mono text-[11px] text-fg-faint">{path || "—"}</p>
          <div className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-ink-900">
            <div className="h-full rounded-full bg-brand" style={{ width: `${Math.max(pct, loc.usedBytes > 0 ? 2 : 0)}%` }} />
          </div>
          <div className="mt-1 flex justify-between text-[11px] text-fg-faint tabular-nums">
            <span>
              {formatBytes(loc.usedBytes)}
              {cap > 0 ? ` of ${formatBytes(cap)}` : " used"} · {loc.cameraCount}{" "}
              {loc.cameraCount === 1 ? "camera" : "cameras"}
            </span>
            {loc.diskFreeBytes != null && <span>{formatBytes(loc.diskFreeBytes)} free</span>}
          </div>
        </div>
        <div className="shrink-0">
          {loc.isDefault ? (
            <button className="btn-subtle h-8" onClick={onAdjustPrimary}>
              <ShieldCheck className="h-3.5 w-3.5" />
              Reserve
            </button>
          ) : (
            <button className="btn-icon text-fg-faint hover:text-rec" title="Remove destination" onClick={onDelete}>
              <Trash2 className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

/* ---------- Step 2: Choose type ---------- */

function TypeStep({
  value,
  onPick,
  onBack,
  onNext,
}: {
  value: LocType;
  onPick: (t: LocType) => void;
  onBack: () => void;
  onNext: () => void;
}) {
  const types: { id: LocType; icon: typeof HardDrive; title: string; sub: string; soon?: boolean }[] = [
    { id: "local", icon: HardDrive, title: "Local folder", sub: "A disk or folder on this machine" },
    { id: "smb", icon: Server, title: "SMB / Windows share", sub: "A NAS or shared network folder" },
    { id: "sftp", icon: Cloud, title: "SFTP / SSH", sub: "A remote server over SSH" },
  ];
  return (
    <div className="p-5">
      <h3 className="text-base font-semibold text-fg">Choose a destination type</h3>
      <p className="mt-1 text-sm text-fg-muted">Record to a local disk or a network share. Credentials are encrypted at rest.</p>

      <div className="mt-4 space-y-2.5">
        {types.map((t) => {
          const active = value === t.id;
          const disabled = !!t.soon;
          return (
            <button
              key={t.id}
              disabled={disabled}
              onClick={() => onPick(t.id)}
              className={`flex w-full items-center gap-3 rounded-lg border p-3 text-left transition-colors ${
                active ? "border-brand bg-brand/10" : "border-line bg-ink-850 hover:bg-ink-800"
              } ${disabled ? "cursor-not-allowed opacity-55" : ""}`}
            >
              <div className={`grid h-10 w-10 shrink-0 place-items-center rounded-lg ${active ? "bg-brand/20" : "bg-ink-900"}`}>
                <t.icon className={`h-5 w-5 ${active ? "text-brand" : "text-fg-muted"}`} />
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium text-fg">{t.title}</span>
                  {t.soon && (
                    <span className="rounded bg-ink-900 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-fg-faint">
                      Soon
                    </span>
                  )}
                </div>
                <p className="text-[12px] text-fg-faint">{t.sub}</p>
              </div>
              {active && <Check className="h-4 w-4 shrink-0 text-brand" />}
            </button>
          );
        })}
      </div>

      <div className="flex gap-2 mt-5">
        <button className="btn-ghost h-9" onClick={onBack}>
          <ChevronLeft className="h-4 w-4" />
          Back
        </button>
        <button className="btn-primary h-9 flex-1" onClick={onNext}>
          Continue
          <ChevronRight className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}

/* ---------- Step 3: Details ---------- */

const TYPE_META: Record<LocType, { icon: typeof HardDrive; title: string; sub: string }> = {
  local: {
    icon: HardDrive,
    title: "Local folder",
    sub: "Point Aperture at a folder on a mounted disk. It’s created if it doesn’t exist yet.",
  },
  smb: {
    icon: Server,
    title: "SMB / Windows share",
    sub: "Connect to a NAS or shared folder. Your password is encrypted at rest.",
  },
  sftp: {
    icon: Cloud,
    title: "SFTP / SSH",
    sub: "Connect to a server over SSH. Your credentials are encrypted at rest.",
  },
};

function DetailsStep({
  draft,
  setDraft,
  onBack,
  onNext,
}: {
  draft: Draft;
  setDraft: (fn: (d: Draft) => Draft) => void;
  onBack: () => void;
  onNext: () => void;
}) {
  const cfg = (k: string) => String(draft.config[k] ?? "");
  const setCfg = (k: string, v: string | number) => setDraft((d) => ({ ...d, config: { ...d.config, [k]: v } }));
  const setName = (v: string) => setDraft((d) => ({ ...d, name: v }));
  const setSecret = (v: string) => setDraft((d) => ({ ...d, secret: v }));
  const meta = TYPE_META[draft.type];

  const valid =
    draft.name.trim() !== "" &&
    (draft.type === "local"
      ? cfg("path").trim() !== ""
      : draft.type === "smb"
        ? cfg("host").trim() !== "" && cfg("share").trim() !== "" && cfg("user").trim() !== ""
        : cfg("host").trim() !== "" && cfg("user").trim() !== "" && draft.secret.trim() !== "");

  return (
    <div className="p-5">
      <div className="mx-auto mb-3 grid h-12 w-12 place-items-center rounded-2xl bg-brand/15">
        <meta.icon className="h-6 w-6 text-brand" />
      </div>
      <h3 className="text-center text-base font-semibold text-fg">{meta.title}</h3>
      <p className="mx-auto mt-1 max-w-md text-center text-sm text-fg-muted">{meta.sub}</p>

      <div className="mt-4 space-y-3">
        <div>
          <label className="label">Name</label>
          <input className="input" placeholder="e.g. NAS – bay 2" value={draft.name} onChange={(e) => setName(e.target.value)} />
        </div>

        {draft.type === "local" && (
          <div>
            <label className="label">Folder path</label>
            <input
              className="input font-mono text-xs"
              placeholder="/mnt/nas2/aperture"
              value={cfg("path")}
              onChange={(e) => setCfg("path", e.target.value)}
            />
            <p className="mt-1 text-[11px] text-fg-faint">Must be writable by the container — mount the disk or share there.</p>
          </div>
        )}

        {draft.type === "smb" && (
          <>
            <div className="grid grid-cols-3 gap-2">
              <div className="col-span-2">
                <label className="label">Host</label>
                <input className="input" placeholder="192.168.1.10" value={cfg("host")} onChange={(e) => setCfg("host", e.target.value)} />
              </div>
              <div>
                <label className="label">Port</label>
                <input className="input" type="number" placeholder="445" value={cfg("port")} onChange={(e) => setCfg("port", e.target.value ? Number(e.target.value) : "")} />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <div>
                <label className="label">Share</label>
                <input className="input" placeholder="recordings" value={cfg("share")} onChange={(e) => setCfg("share", e.target.value)} />
              </div>
              <div>
                <label className="label">Subfolder</label>
                <input className="input" placeholder="aperture (optional)" value={cfg("subfolder")} onChange={(e) => setCfg("subfolder", e.target.value)} />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <div>
                <label className="label">Username</label>
                <input className="input" value={cfg("user")} onChange={(e) => setCfg("user", e.target.value)} />
              </div>
              <div>
                <label className="label">Domain</label>
                <input className="input" placeholder="optional" value={cfg("domain")} onChange={(e) => setCfg("domain", e.target.value)} />
              </div>
            </div>
            <div>
              <label className="label">Password</label>
              <input className="input" type="password" value={draft.secret} onChange={(e) => setSecret(e.target.value)} />
            </div>
          </>
        )}

        {draft.type === "sftp" && (
          <>
            <div className="grid grid-cols-3 gap-2">
              <div className="col-span-2">
                <label className="label">Host</label>
                <input className="input" placeholder="host.example.com" value={cfg("host")} onChange={(e) => setCfg("host", e.target.value)} />
              </div>
              <div>
                <label className="label">Port</label>
                <input className="input" type="number" placeholder="22" value={cfg("port")} onChange={(e) => setCfg("port", e.target.value ? Number(e.target.value) : "")} />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <div>
                <label className="label">Username</label>
                <input className="input" value={cfg("user")} onChange={(e) => setCfg("user", e.target.value)} />
              </div>
              <div>
                <label className="label">Remote path</label>
                <input className="input font-mono text-xs" placeholder="/srv/aperture" value={cfg("path")} onChange={(e) => setCfg("path", e.target.value)} />
              </div>
            </div>
            <div>
              <label className="label">Authentication</label>
              <select className="input" value={cfg("authMode") || "password"} onChange={(e) => setCfg("authMode", e.target.value)}>
                <option value="password">Password</option>
                <option value="key">Private key</option>
              </select>
            </div>
            <div>
              <label className="label">{cfg("authMode") === "key" ? "Private key (PEM)" : "Password"}</label>
              {cfg("authMode") === "key" ? (
                <textarea
                  className="input font-mono text-[11px]"
                  rows={4}
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                  value={draft.secret}
                  onChange={(e) => setSecret(e.target.value)}
                />
              ) : (
                <input className="input" type="password" value={draft.secret} onChange={(e) => setSecret(e.target.value)} />
              )}
            </div>
          </>
        )}
      </div>

      <div className="flex gap-2 mt-5">
        <button className="btn-ghost h-9" onClick={onBack}>
          <ChevronLeft className="h-4 w-4" />
          Back
        </button>
        <button className="btn-primary h-9 flex-1" disabled={!valid} onClick={onNext}>
          Test connection
          <ChevronRight className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}

/* ---------- Step 4: Connection checklist ---------- */

const SKELETONS: Record<LocType, { id: string; label: string }[]> = {
  local: [
    { id: "reach", label: "Reaching the folder" },
    { id: "write", label: "Writing a test file" },
    { id: "read", label: "Reading it back" },
    { id: "capacity", label: "Reading capacity" },
  ],
  smb: [
    { id: "connect", label: "Connecting & signing in" },
    { id: "mount", label: "Opening the share" },
    { id: "subfolder", label: "Preparing the folder" },
    { id: "write", label: "Writing a test file" },
    { id: "read", label: "Reading it back" },
    { id: "capacity", label: "Reading capacity" },
  ],
  sftp: [
    { id: "connect", label: "Connecting & signing in" },
    { id: "sftp", label: "Starting SFTP" },
    { id: "folder", label: "Preparing the folder" },
    { id: "write", label: "Writing a test file" },
    { id: "read", label: "Reading it back" },
    { id: "capacity", label: "Reading capacity" },
  ],
};

function TestingStep({
  draft,
  onBack,
  onPass,
}: {
  draft: Draft;
  onBack: () => void;
  onPass: (free: number, total: number, hostKey?: string) => void;
}) {
  const test = useTestLocation();
  const [steps, setSteps] = useState<TestStep[]>([]);
  const [revealed, setRevealed] = useState(0);
  const [done, setDone] = useState(false);
  const [ok, setOk] = useState(false);
  const [free, setFree] = useState(0);
  const [total, setTotal] = useState(0);
  const [hostKey, setHostKey] = useState<string | undefined>();

  // Kick off the probe once on mount.
  useEffect(() => {
    let cancelled = false;
    setSteps([]);
    setRevealed(0);
    setDone(false);
    setOk(false);
    (async () => {
      try {
        const res = await test.mutateAsync({
          name: draft.name,
          type: draft.type,
          config: draft.config,
          secret: draft.secret || undefined,
          // Editing an existing destination: an empty secret means "keep the
          // stored one" — the server decrypts it for the probe.
          locationId: draft.editId || undefined,
        });
        if (cancelled) return;
        setSteps(res.steps);
        setOk(res.ok);
        setFree(res.diskFreeBytes ?? 0);
        setTotal(res.diskTotalBytes ?? 0);
        setHostKey(res.hostKey);
      } catch (e) {
        if (cancelled) return;
        setSteps([
          {
            id: "error",
            label: "Connect",
            status: "fail",
            detail: e instanceof Error ? e.message : "The check could not run.",
          },
        ]);
        setOk(false);
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // The row plan: the per-type skeleton (shown as pending up front). If the
  // backend returns a shorter shape (a short-circuit fail), the extra rows resolve
  // to "skip".
  const plan = SKELETONS[draft.type] ?? (steps.length ? steps.map((s) => ({ id: s.id, label: s.label })) : [{ id: "connect", label: "Connecting" }]);
  const planLen = plan.length;

  // Reveal the resolved steps one at a time for a live, sequential feel.
  useEffect(() => {
    if (steps.length === 0) return;
    if (revealed >= planLen) {
      const t = window.setTimeout(() => setDone(true), 350);
      return () => window.clearTimeout(t);
    }
    const t = window.setTimeout(() => setRevealed((n) => n + 1), 480);
    return () => window.clearTimeout(t);
  }, [steps, revealed, planLen]);

  const rows: { id: string; label: string; status: string; detail?: string }[] = plan.map((p, i) => {
    if (steps.length === 0 || i >= revealed) return { ...p, status: "run" };
    const resolved = steps[i];
    if (resolved) return resolved;
    return { ...p, status: "skip", detail: "Skipped" };
  });

  return (
    <div className="p-5">
      <h3 className="text-base font-semibold text-fg">Testing the connection</h3>
      <p className="mt-1 text-sm text-fg-muted font-mono text-[12px] truncate">{connectionSummary(draft)}</p>

      <div className="mt-4 space-y-1.5">
        {rows.map((s, i) => (
          <ChecklistRow key={`${s.id}-${i}`} step={s} />
        ))}
      </div>

      {done && (
        <div
          className={`mt-4 flex items-center gap-2 rounded-lg border p-3 text-sm ${
            ok ? "border-online/30 bg-online/10 text-fg" : "border-rec/30 bg-rec/10 text-fg"
          }`}
        >
          {ok ? <Check className="h-4 w-4 text-online" /> : <AlertTriangle className="h-4 w-4 text-rec" />}
          {ok ? "Connection verified — this destination is ready." : "Couldn’t verify the connection. Adjust and retry."}
        </div>
      )}

      <div className="flex gap-2 mt-5">
        <button className="btn-ghost h-9" onClick={onBack}>
          <ChevronLeft className="h-4 w-4" />
          Back
        </button>
        <button className="btn-primary h-9 flex-1" disabled={!done || !ok} onClick={() => onPass(free, total, hostKey)}>
          Continue
          <ChevronRight className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}

function ChecklistRow({ step }: { step: { label: string; status: string; detail?: string } }) {
  const icon =
    step.status === "pass" ? (
      <div className="grid h-5 w-5 place-items-center rounded-full bg-online/20">
        <Check className="h-3 w-3 text-online" />
      </div>
    ) : step.status === "fail" ? (
      <div className="grid h-5 w-5 place-items-center rounded-full bg-rec/20">
        <X className="h-3 w-3 text-rec" />
      </div>
    ) : step.status === "warn" ? (
      <div className="grid h-5 w-5 place-items-center rounded-full bg-amber/20">
        <AlertTriangle className="h-3 w-3 text-amber" />
      </div>
    ) : step.status === "skip" ? (
      <div className="h-5 w-5 rounded-full border border-line" />
    ) : (
      <Loader2 className="h-5 w-5 animate-spin text-brand" />
    );
  const dim = step.status === "skip" || step.status === "run";
  return (
    <div className="flex items-center gap-2.5 rounded-lg bg-ink-850 px-3 py-2">
      {icon}
      <span className={`text-sm ${dim ? "text-fg-faint" : "text-fg"}`}>{step.label}</span>
      {step.detail && <span className="ml-auto truncate text-[11px] text-fg-faint">{step.detail}</span>}
    </div>
  );
}

/* ---------- Step 5: Cameras + allocation ---------- */

function AllocateStep({
  draft,
  setDraft,
  cameras,
  locations,
  perCamBps,
  saving,
  error,
  onBack,
  onSave,
}: {
  draft: Draft;
  setDraft: (fn: (d: Draft) => Draft) => void;
  cameras: { id: string; name: string; locationId: string }[];
  locations: StorageLocation[];
  perCamBps: number;
  saving: boolean;
  error: string;
  onBack: () => void;
  onSave: () => void;
}) {
  const editing = !!draft.editId;
  const isNetwork = draft.type === "smb" || draft.type === "sftp";
  // Known free space caps the slider; when a share doesn't report capacity, allow
  // a generous soft-cap range.
  const maxAlloc = draft.diskFreeBytes > 0 ? draft.diskFreeBytes : 8 * 1024 ** 4;
  const selected = new Set(draft.cameraIds);
  const rate = perCamBps * Math.max(1, draft.cameraIds.length);
  const holdSec = rate > 0 ? draft.allocationBytes / rate : 0;
  const locName = (id: string) => locations.find((l) => l.id === id)?.name || "Primary";

  function toggle(id: string) {
    setDraft((d) => {
      const s = new Set(d.cameraIds);
      s.has(id) ? s.delete(id) : s.add(id);
      return { ...d, cameraIds: [...s] };
    });
  }

  return (
    <div className="p-5">
      <h3 className="text-base font-semibold text-fg">Cameras &amp; space</h3>
      <p className="mt-1 text-sm text-fg-muted">
        Choose which cameras record to <span className="text-fg font-medium">{draft.name || "this destination"}</span>{" "}
        and how much space it may use.
      </p>

      {/* Allocation slider */}
      <div className="mt-4 rounded-lg border border-line bg-ink-850 p-3">
        <div className="flex items-end justify-between mb-2">
          <label className="label mb-0">{isNetwork ? "Allocation (soft cap)" : "Allocation (hard-reserved)"}</label>
          <span className="text-lg font-semibold text-fg tabular-nums">{formatBytes(draft.allocationBytes)}</span>
        </div>
        <input
          type="range"
          min={GiB}
          max={maxAlloc}
          step={GiB / 2}
          value={clamp(draft.allocationBytes, GiB, maxAlloc)}
          onChange={(e) => setDraft((d) => ({ ...d, allocationBytes: clamp(Number(e.target.value), GiB, maxAlloc) }))}
          className="w-full accent-brand"
        />
        <div className="flex justify-between text-[11px] text-fg-faint mt-1">
          <span>1 GB</span>
          <span>{draft.diskFreeBytes > 0 ? `Up to ${formatBytes(draft.diskFreeBytes)} free` : "Capacity unknown"}</span>
        </div>
        <div className="mt-2.5 flex items-center gap-2 text-[12px] text-fg-muted">
          <Clock className="h-3.5 w-3.5 text-brand shrink-0" />
          {draft.cameraIds.length > 0 && perCamBps > 0 ? (
            <span>
              Holds ≈ <span className="text-fg font-medium">{fmtDuration(holdSec)}</span> for{" "}
              {draft.cameraIds.length} {draft.cameraIds.length === 1 ? "camera" : "cameras"}
            </span>
          ) : (
            <span>Pick cameras below to estimate how long it holds.</span>
          )}
        </div>
      </div>

      {/* Camera picker (adding only — while editing, memberships are managed on
          the destination cards, where mirrors are visible too) */}
      {editing ? (
        <p className="mt-4 text-[11px] text-fg-faint">
          Cameras keep their current assignments — manage them on the destination card.
        </p>
      ) : (
      <>
      <p className="mt-4 mb-2 text-xs font-medium text-fg-muted">Cameras to record here</p>
      <div className="space-y-1.5 max-h-52 overflow-y-auto pr-1">
        {cameras.length === 0 && <p className="text-sm text-fg-faint">No cameras yet.</p>}
        {cameras.map((c) => {
          const on = selected.has(c.id);
          const currently = c.locationId && c.locationId !== "" ? locName(c.locationId) : "Primary";
          return (
            <button
              key={c.id}
              onClick={() => toggle(c.id)}
              className={`flex w-full items-center gap-3 rounded-lg border p-2.5 text-left transition-colors ${
                on ? "border-brand bg-brand/10" : "border-line bg-ink-850 hover:bg-ink-800"
              }`}
            >
              <div className={`grid h-5 w-5 shrink-0 place-items-center rounded ${on ? "bg-brand" : "border border-line"}`}>
                {on && <Check className="h-3.5 w-3.5 text-white" />}
              </div>
              <Video className="h-4 w-4 shrink-0 text-fg-faint" />
              <span className="flex-1 truncate text-sm text-fg">{c.name}</span>
              <span className="shrink-0 text-[11px] text-fg-faint">now: {currently}</span>
            </button>
          );
        })}
      </div>
      </>
      )}

      {error && <p className="mt-3 text-xs text-rec">{error}</p>}

      <div className="flex gap-2 mt-5">
        <button className="btn-ghost h-9" onClick={onBack} disabled={saving}>
          <ChevronLeft className="h-4 w-4" />
          Back
        </button>
        <button className="btn-primary h-9 flex-1" onClick={onSave} disabled={saving}>
          {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
          {saving ? "Saving…" : editing ? "Apply changes" : "Save destination"}
        </button>
      </div>
    </div>
  );
}

/* ---------- Step 6: Done ---------- */

function DoneStep({ draft, onAddAnother, onClose }: { draft: Draft; onAddAnother: () => void; onClose: () => void }) {
  return (
    <div className="p-6 text-center">
      <div className="mx-auto mb-4 grid h-14 w-14 place-items-center rounded-2xl bg-online/15">
        <Check className="h-7 w-7 text-online" />
      </div>
      <h3 className="text-lg font-semibold text-fg">{draft.name} is ready</h3>
      <p className="mx-auto mt-2 max-w-md text-sm text-fg-muted leading-relaxed">
        {draft.cameraIds.length > 0
          ? `${draft.cameraIds.length} ${draft.cameraIds.length === 1 ? "camera" : "cameras"} now record here.`
          : "No cameras are pointed here yet — assign them any time from a camera’s settings."}{" "}
        {draft.allocationBytes > 0 &&
          `Up to ${formatBytes(draft.allocationBytes)} is hard-reserved; the space fills in the background.`}
      </p>
      <div className="flex gap-2 mt-6">
        <button className="btn-ghost h-9 flex-1" onClick={onAddAnother}>
          <Plus className="h-4 w-4" />
          Add another
        </button>
        <button className="btn-primary h-9 flex-1" onClick={onClose}>
          Done
        </button>
      </div>
    </div>
  );
}
