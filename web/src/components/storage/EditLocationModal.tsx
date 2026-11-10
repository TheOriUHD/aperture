import { useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { Modal } from "../ui/Modal";
import { Switch } from "../ui/Switch";
import { useDiskStats } from "../../hooks/useStorage";
import type { StorageLocation } from "../../hooks/useStorage";
import {
  useUpdateLocation,
  useAllocateLocation,
  usePreviewAllocation,
  useSetCameraDestinations,
  useStorageLocations,
  type EvictionPreview,
} from "../../hooks/useStorageLocations";
import { useCameras } from "../../hooks/useCameras";
import { formatBytes } from "../../lib/format";
import { EvictionConfirm } from "./EvictionConfirm";

const GiB = 1024 ** 3;
const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));

// One modal to edit any destination: Primary (allocation, bounded by the disk) or
// a network share (name, allocation, credentials, enable). Changes apply
// immediately — the allocation is enforced without a restart.
export function EditLocationModal({
  loc,
  onClose,
  onReconfigure,
}: {
  loc: StorageLocation | null;
  onClose: () => void;
  onReconfigure?: () => void;
}) {
  const open = !!loc;
  const isDefault = loc?.isDefault ?? false;
  const isNetwork = loc?.type === "smb" || loc?.type === "sftp";
  const { data: disk } = useDiskStats(open && isDefault);
  const update = useUpdateLocation();
  const allocate = useAllocateLocation();
  const preview = usePreviewAllocation();
  const setDest = useSetCameraDestinations();
  const { data: locData } = useStorageLocations();
  const { data: allCameras } = useCameras();

  const [name, setName] = useState("");
  const [alloc, setAlloc] = useState(0);
  const [enabled, setEnabled] = useState(true);
  const [err, setErr] = useState("");
  // Which cameras record to THIS destination. A camera ticked on several
  // destinations records to all of them at once (same footage everywhere).
  const [ticked, setTicked] = useState<Set<string>>(new Set());
  // Non-null once a DECREASE has been previewed and is awaiting confirmation.
  const [pending, setPending] = useState<EvictionPreview | null>(null);

  useEffect(() => {
    if (!loc) return;
    setName(loc.name);
    setAlloc(loc.allocationBytes);
    setEnabled(loc.enabled);
    setTicked(new Set((loc.cameras ?? []).map((c) => c.cameraId)));
    setErr("");
    setPending(null);
  }, [loc]);

  if (!loc) return null;

  // Allocation ceiling: local Primary is bound by what the disk can reserve;
  // network by its reported free space (or generous when capacity is unknown).
  const maxAlloc = isDefault
    ? Math.max(GiB, disk?.maxAllocatableBytes ?? loc.diskFreeBytes ?? 4 * 1024 ** 4)
    : loc.diskFreeBytes && loc.diskFreeBytes > 0
      ? loc.diskFreeBytes
      : 8 * 1024 ** 4;
  const busy = update.isPending || allocate.isPending || preview.isPending || setDest.isPending;

  // Lowering an allocation evicts footage the instant it is applied, with no undo.
  // Ask the server exactly what would be destroyed and make the user confirm it
  // before anything is written. An increase destroys nothing, so it saves directly.
  async function save() {
    setErr("");
    if (alloc < loc!.allocationBytes) {
      try {
        const p = await preview.mutateAsync({ locationId: loc!.id, allocationBytes: alloc });
        if (p.willEvict) {
          setPending(p);
          return;
        }
      } catch (e) {
        setErr(e instanceof Error ? e.message : "Could not check what this would delete.");
        return;
      }
    }
    await commit();
  }

  // membershipsOf: every destination a camera currently records to, across all
  // locations (each location lists its cameras, so the union is authoritative).
  function membershipsOf(camId: string) {
    const out: { locationId: string; retentionDays: number; primary: boolean }[] = [];
    for (const l of locData?.locations ?? []) {
      const m = (l.cameras ?? []).find((c) => c.cameraId === camId);
      if (m) out.push({ locationId: l.id, retentionDays: m.retentionDays, primary: m.primary });
    }
    return out;
  }

  // applyCameraChanges: make each camera's membership match the checkboxes. The
  // "primary" (ffmpeg's write target) is an internal detail — if it was unticked,
  // another ticked destination silently takes over; a camera left with nothing
  // falls back to Primary storage so it always records somewhere.
  async function applyCameraChanges() {
    const before = new Set((loc!.cameras ?? []).map((c) => c.cameraId));
    for (const cam of allCameras ?? []) {
      const was = before.has(cam.id);
      const is = ticked.has(cam.id);
      if (was === is) continue;
      const ms = membershipsOf(cam.id);
      let next = is
        ? [...ms, { locationId: loc!.id, retentionDays: 0, primary: false }]
        : ms.filter((m) => m.locationId !== loc!.id);
      if (next.length === 0) next = [{ locationId: "", retentionDays: 0, primary: true }];
      const prevPrimary = ms.find((m) => m.primary)?.locationId;
      const primary =
        prevPrimary !== undefined && next.some((m) => m.locationId === prevPrimary)
          ? prevPrimary
          : next.some((m) => m.locationId === "")
            ? ""
            : next[0].locationId;
      await setDest.mutateAsync({
        cameraId: cam.id,
        primaryLocationId: primary,
        destinations: next.map((m) => ({ locationId: m.locationId, retentionDays: m.retentionDays })),
      });
    }
  }

  async function commit() {
    setErr("");
    setPending(null);
    try {
      if (isDefault) {
        await allocate.mutateAsync({ locationId: "", allocationBytes: alloc });
        await applyCameraChanges();
      } else {
        await update.mutateAsync({
          id: loc!.id,
          etag: loc!.etag, // If-Match: reject the save if the location changed since we loaded it
          name: name.trim() || loc!.name,
          enabled,
        });
        await allocate.mutateAsync({ locationId: loc!.id, allocationBytes: alloc });
        await applyCameraChanges();
      }
      onClose();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Could not save changes.");
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={isDefault ? "Edit primary storage" : `Edit ${loc.name}`}
      footer={
        <div className="flex w-full items-center justify-end gap-2">
          {err && <span className="mr-auto text-xs text-danger">{err}</span>}
          <button className="btn-ghost" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button className="btn-primary" onClick={save} disabled={busy}>
            {busy && <Loader2 className="h-4 w-4 animate-spin" />}
            Save changes
          </button>
        </div>
      }
    >
      <div className="space-y-5">
        {!isDefault && (
          <div>
            <label className="label">Name</label>
            <input className="input" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
        )}

        {/* Allocation */}
        <div className="rounded-lg border border-line-strong p-4">
          <div className="mb-2 flex items-end justify-between">
            <label className="label mb-0">{isDefault ? "Reserved for Aperture" : isNetwork ? "Allocation (soft cap)" : "Allocation"}</label>
            <span className="text-lg font-semibold tabular-nums text-fg">{formatBytes(alloc)}</span>
          </div>
          <input
            type="range"
            min={0}
            max={maxAlloc}
            step={GiB / 2}
            value={clamp(alloc, 0, maxAlloc)}
            onChange={(e) => setAlloc(clamp(Number(e.target.value), 0, maxAlloc))}
            className="w-full"
            aria-label="Allocation"
          />
          <div className="mt-1 flex justify-between text-[11px] text-fg-faint">
            <span>Off</span>
            <span>
              {isDefault && disk
                ? `Max ${formatBytes(disk.maxAllocatableBytes)}`
                : loc.diskFreeBytes && loc.diskFreeBytes > 0
                  ? `${formatBytes(loc.diskFreeBytes)} free`
                  : "Capacity unknown"}
            </span>
          </div>
          <p className="hint mt-2">
            {isDefault
              ? "Space is hard-reserved on the local disk; oldest footage is trimmed at the limit. 0 = unlimited (safety net only)."
              : isNetwork
                ? "Oldest footage on this share is deleted to stay under the cap. 0 = unlimited."
                : "Hard-reserved on this disk. 0 = unlimited."}
          </p>
        </div>


        {/* Cameras recording to this destination. Ticked on two destinations =
            recorded to both at once, each keeping its own retention. */}
        <div>
          <label className="label">Cameras recording here</label>
          <div className="grid grid-cols-1 gap-x-4 rounded-lg border border-line-strong px-2 py-1.5 sm:grid-cols-2">
            {(allCameras ?? []).map((c) => (
              <label
                key={c.id}
                className="flex h-8 cursor-pointer items-center gap-2.5 rounded-md px-1.5 text-sm text-fg hover:bg-white/[0.03]"
              >
                <input
                  type="checkbox"
                  checked={ticked.has(c.id)}
                  onChange={() =>
                    setTicked((t) => {
                      const n = new Set(t);
                      n.has(c.id) ? n.delete(c.id) : n.add(c.id);
                      return n;
                    })
                  }
                />
                <span className="truncate">{c.name}</span>
              </label>
            ))}
            {(allCameras ?? []).length === 0 && <p className="px-1.5 py-1.5 text-xs text-fg-faint">No cameras yet.</p>}
          </div>
          <p className="hint mt-1.5">
            A camera can be ticked on several destinations — it records to all of them at the
            same time. New footage only; existing recordings stay where they are.
          </p>
        </div>

        {/* Connection changes (host, share, credentials) go through the setup
            wizard so every change is PROBED before it is saved — an untested
            config can no longer be written from here. */}
        {isNetwork && (
          <div className="divide-y divide-line-strong rounded-lg border border-line-strong">
            <div className="flex items-center justify-between gap-3 px-4 py-3">
              <div className="min-w-0">
                <p className="text-sm text-fg">Connection &amp; credentials</p>
                <p className="hint">
                  {String(loc.config?.host ?? "")} — changes are tested before they apply.
                </p>
              </div>
              <button className="btn-ghost shrink-0" onClick={onReconfigure}>
                Reconfigure…
              </button>
            </div>
            <div className="flex items-center justify-between gap-3 px-4 py-3">
              <div className="min-w-0">
                <p className="text-sm text-fg">Enabled</p>
                <p className="hint">Turn off to pause recording to this destination.</p>
              </div>
              <Switch checked={enabled} onChange={setEnabled} label="Enabled" />
            </div>
          </div>
        )}

        {isDefault && (
          <p className="hint">
            The app and database always live with the install and aren’t affected by this limit.
          </p>
        )}
      </div>

      <EvictionConfirm
        preview={pending}
        busy={busy}
        onCancel={() => setPending(null)}
        onConfirm={commit}
      />
    </Modal>
  );
}

