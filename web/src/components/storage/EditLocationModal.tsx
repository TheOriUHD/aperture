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
  type EvictionPreview,
} from "../../hooks/useStorageLocations";
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

  const [name, setName] = useState("");
  const [alloc, setAlloc] = useState(0);
  const [enabled, setEnabled] = useState(true);
  const [err, setErr] = useState("");
  // Non-null once a DECREASE has been previewed and is awaiting confirmation.
  const [pending, setPending] = useState<EvictionPreview | null>(null);

  useEffect(() => {
    if (!loc) return;
    setName(loc.name);
    setAlloc(loc.allocationBytes);
    setEnabled(loc.enabled);
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
  const busy = update.isPending || allocate.isPending || preview.isPending;

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

  async function commit() {
    setErr("");
    setPending(null);
    try {
      if (isDefault) {
        await allocate.mutateAsync({ locationId: "", allocationBytes: alloc });
      } else {
        await update.mutateAsync({
          id: loc!.id,
          etag: loc!.etag, // If-Match: reject the save if the location changed since we loaded it
          name: name.trim() || loc!.name,
          enabled,
        });
        await allocate.mutateAsync({ locationId: loc!.id, allocationBytes: alloc });
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
        <div className="flex items-center justify-end gap-2">
          {err && <span className="mr-auto text-xs text-rec">{err}</span>}
          <button className="btn-ghost h-9" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button className="btn-primary h-9" onClick={save} disabled={busy}>
            {busy && <Loader2 className="h-4 w-4 animate-spin" />}
            Save changes
          </button>
        </div>
      }
    >
      <div className="space-y-4">
        {!isDefault && (
          <div>
            <label className="label">Name</label>
            <input className="input" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
        )}

        {/* Allocation */}
        <div className="rounded-lg border border-line bg-ink-850 p-3">
          <div className="flex items-end justify-between mb-2">
            <label className="label mb-0">{isDefault ? "Reserved for Aperture" : isNetwork ? "Allocation (soft cap)" : "Allocation"}</label>
            <span className="text-lg font-semibold text-fg tabular-nums">{formatBytes(alloc)}</span>
          </div>
          <input
            type="range"
            min={0}
            max={maxAlloc}
            step={GiB / 2}
            value={clamp(alloc, 0, maxAlloc)}
            onChange={(e) => setAlloc(clamp(Number(e.target.value), 0, maxAlloc))}
            className="w-full accent-brand"
          />
          <div className="flex justify-between text-[11px] text-fg-faint mt-1">
            <span>Off</span>
            <span>
              {isDefault && disk
                ? `Max ${formatBytes(disk.maxAllocatableBytes)}`
                : loc.diskFreeBytes && loc.diskFreeBytes > 0
                  ? `${formatBytes(loc.diskFreeBytes)} free`
                  : "Capacity unknown"}
            </span>
          </div>
          <p className="mt-2 text-[11px] text-fg-faint">
            {isDefault
              ? "Space is hard-reserved on the local disk; oldest footage is trimmed at the limit. 0 = unlimited (safety net only)."
              : isNetwork
                ? "Oldest footage on this share is deleted to stay under the cap. 0 = unlimited."
                : "Hard-reserved on this disk. 0 = unlimited."}
          </p>
        </div>

        {/* Connection changes (host, share, credentials) go through the setup
            wizard so every change is PROBED before it is saved — an untested
            config can no longer be written from here. */}
        {isNetwork && (
          <div className="space-y-3">
            <div className="flex items-center justify-between rounded-lg border border-line bg-ink-850 px-3 py-2">
              <div>
                <p className="text-sm text-fg">Connection &amp; credentials</p>
                <p className="text-[11px] text-fg-faint">
                  {String(loc.config?.host ?? "")} — changes are tested before they apply.
                </p>
              </div>
              <button className="btn-subtle h-8" onClick={onReconfigure}>
                Reconfigure…
              </button>
            </div>
            <div className="flex items-center justify-between rounded-lg border border-line bg-ink-850 px-3 py-2">
              <div>
                <p className="text-sm text-fg">Enabled</p>
                <p className="text-[11px] text-fg-faint">Turn off to pause recording to this destination.</p>
              </div>
              <Switch checked={enabled} onChange={setEnabled} />
            </div>
          </div>
        )}

        {isDefault && (
          <p className="text-[11px] text-fg-faint">
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

