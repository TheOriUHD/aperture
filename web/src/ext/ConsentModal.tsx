import { useEffect, useState } from "react";
import { Puzzle, AlertTriangle, ShieldCheck, Cctv, Settings2, RefreshCw } from "lucide-react";
import { Modal } from "../components/ui/Modal";
import { PermissionList } from "./PermissionList";
import { CameraGrantControl, initialGrantValue } from "./CameraGrants";
import type { StageView } from "./types";

// The consent decision, as a modal over the manager. Identity on top; permissions in
// plain language, weighted so the consequential grants stand out; camera scope is the
// user's to narrow here (the manifest is a request, not a setting); the sideload
// warning is unmissable for non-store installs; the buttons are weighted honestly. An
// update reads as a replacement of a specific version, never as a first install.

export function ConsentModal({
  stage,
  busy,
  error,
  mode = "install",
  onConfirm,
  onCancel,
}: {
  stage: StageView | null;
  busy?: boolean;
  error?: string;
  mode?: "install" | "update";
  onConfirm: (cameras: string[] | null | undefined, allowDowngrade?: boolean) => void;
  onCancel: () => void;
}) {
  const open = !!stage;
  const store = stage?.source === "store";
  const isUpdate = mode === "update" || !!stage?.update;
  const downgrade = !!stage?.downgrade;
  const permsChanged = !!stage?.permissionsChanged;

  const requestAll = (stage?.requestsCameras ?? false) && (stage?.requestedCameras ?? []).length === 0;
  const [grant, setGrant] = useState<string[] | null>(null);
  const [downgradeAck, setDowngradeAck] = useState(false);
  // Default to what the manifest requested each time a fresh bundle is staged.
  useEffect(() => {
    setGrant(initialGrantValue(requestAll, stage?.requestedCameras ?? []));
    setDowngradeAck(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stage?.stagingId, requestAll]);

  // Narrowing happens at first install; an update preserves the user's existing grants
  // (editable from the row), so the picker doesn't appear on an update.
  const needsCamera = !!stage?.requestsCameras && !isUpdate;
  // null = "all cameras (incl. future)"; a non-empty list is a valid choice; an empty
  // list means "specific" with nothing selected, which is not installable.
  const cameraOk = !needsCamera || grant === null || grant.length > 0;
  const canConfirm = cameraOk && (!downgrade || downgradeAck);
  const cameras = needsCamera ? grant : undefined;

  return (
    <Modal
      open={open}
      onClose={onCancel}
      title={isUpdate ? "Review update" : "Install extension"}
      footer={
        <>
          <button className="btn-ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </button>
          <button
            className="btn-primary"
            onClick={() => onConfirm(cameras, downgrade ? true : undefined)}
            disabled={busy || !canConfirm}
          >
            {busy
              ? isUpdate
                ? "Updating…"
                : "Installing…"
              : downgrade
                ? `Downgrade to v${stage?.version}`
                : isUpdate
                  ? "Update"
                  : "Install"}
          </button>
        </>
      }
    >
      {stage && (
        <div className="space-y-4">
          <div className="flex items-start gap-3">
            <div className="grid h-10 w-10 shrink-0 place-items-center rounded-lg border border-line bg-ink-700 text-fg-muted">
              <Puzzle className="h-5 w-5" strokeWidth={1.75} />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <h3 className="truncate text-[15px] font-semibold text-fg">{stage.name}</h3>
                <span className="pill-gray">v{stage.version}</span>
                {store ? (
                  <span className="pill-green">
                    <ShieldCheck className="h-3 w-3" /> Store
                  </span>
                ) : (
                  <span className="pill-orange">Sideloaded</span>
                )}
              </div>
              <p className="mt-1 text-xs leading-relaxed text-fg-muted">
                {stage.author ? `by ${stage.author}` : "Unknown author"}
                {stage.description ? ` · ${stage.description}` : ""}
              </p>
            </div>
          </div>

          {isUpdate && stage.currentVersion && (
            <div className="flex items-start gap-2.5 rounded-md border border-brand/30 bg-brand/[0.08] px-3 py-2.5 text-sm text-fg">
              <RefreshCw className="mt-0.5 h-4 w-4 shrink-0 text-brand" />
              <span>
                Replacing <b>version {stage.currentVersion}</b> with <b>version {stage.version}</b>. Its settings and
                camera choice are kept; the current files are snapshotted so you can revert.
              </span>
            </div>
          )}

          {!store && (
            <div className="flex items-start gap-2.5 rounded-md border border-danger/30 bg-danger/[0.08] px-3 py-2.5 text-sm text-fg">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-danger" />
              <span>
                <b>Sideloaded extension.</b> This came from a file or URL you provided and has{" "}
                <b>not been reviewed by anyone</b>. Only continue if you trust where it came from.
              </span>
            </div>
          )}

          {downgrade && (
            <label className="flex cursor-pointer items-start gap-2.5 rounded-md border border-warn/40 bg-warn/[0.08] px-3 py-2.5 text-sm text-fg">
              <input
                type="checkbox"
                className="mt-0.5 h-4 w-4 shrink-0 accent-warn"
                checked={downgradeAck}
                onChange={(e) => setDowngradeAck(e.target.checked)}
              />
              <span>
                <b>This is a downgrade.</b> Version {stage.version} is older than the installed{" "}
                {stage.currentVersion}. Install the older version anyway.
              </span>
            </label>
          )}

          <div>
            <p className="mb-2 text-[13px] font-semibold text-fg">
              {isUpdate ? "This update will be allowed to:" : "This extension will be allowed to:"}
            </p>
            {permsChanged && (
              <p className="mb-2 flex items-center gap-1.5 text-xs text-warn">
                <AlertTriangle className="h-3 w-3" /> This version requests permissions the installed one didn't — review them.
              </p>
            )}
            <PermissionList lines={stage.consent} />
          </div>

          {needsCamera && (
            <div className="border-t border-line pt-4">
              <div className="mb-3 flex items-start gap-2.5">
                <Cctv className="mt-0.5 h-4 w-4 shrink-0 text-fg-muted" />
                <div>
                  <p className="text-[13px] font-semibold text-fg">{stage.cameraConsent || "Cameras"}</p>
                  <p className="mt-0.5 text-xs text-fg-faint">
                    You decide which cameras — the request is a default you can narrow now or from its settings later.
                  </p>
                </div>
              </div>
              <CameraGrantControl
                requestsCameras={stage.requestsCameras}
                requestedCameras={stage.requestedCameras}
                value={grant}
                onChange={setGrant}
              />
            </div>
          )}

          {stage.hasSettings && !isUpdate && (
            <p className="flex items-start gap-2 text-xs text-fg-faint">
              <Settings2 className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              After installing, configure this extension from its row in Extensions before it can run.
            </p>
          )}

          {error && <p className="text-sm text-danger">{error}</p>}
        </div>
      )}
    </Modal>
  );
}
