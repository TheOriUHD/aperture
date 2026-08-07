import { useEffect, useState } from "react";
import { Puzzle, AlertTriangle, ShieldCheck, Cctv } from "lucide-react";
import { Modal } from "../components/ui/Modal";
import { PermissionList } from "./PermissionList";
import { CameraGrantList, useCameraChoices, grantsToSend, initialSelection } from "./CameraGrants";
import type { StageView } from "./types";

// The consent decision, as a modal over the manager. Identity on top; permissions in
// plain language, weighted so the consequential grants stand out; camera scope is the
// user's to narrow here (the manifest is a request, not a setting); the sideload
// warning is unmissable for non-store installs; the buttons are weighted honestly.

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
  onConfirm: (cameras: string[] | null | undefined) => void;
  onCancel: () => void;
}) {
  const open = !!stage;
  const store = stage?.source === "store";

  const { choices, requestAll } = useCameraChoices(stage?.requestsCameras ?? false, stage?.requestedCameras ?? []);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  // Default to what the manifest requested (all choices checked) each time a fresh
  // bundle is staged or its cameras resolve.
  useEffect(() => {
    setSelected(initialSelection(choices, null));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stage?.stagingId, choices.length]);

  // Narrowing happens at install; an update preserves the user's existing grants
  // (editable from the row), so the picker doesn't appear on the update re-consent.
  const needsCamera = !!stage?.requestsCameras && mode === "install";
  const canConfirm = !needsCamera || selected.size > 0;
  const cameras = needsCamera ? grantsToSend(selected, choices, requestAll) : undefined;

  return (
    <Modal
      open={open}
      onClose={onCancel}
      title={mode === "update" ? "Review update" : "Install extension"}
      footer={
        <>
          <button className="btn-ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </button>
          <button className="btn-primary" onClick={() => onConfirm(cameras)} disabled={busy || !canConfirm}>
            {busy ? (mode === "update" ? "Updating…" : "Installing…") : mode === "update" ? "Update" : "Install"}
          </button>
        </>
      }
    >
      {stage && (
        <div className="space-y-4">
          <div className="flex items-start gap-3">
            <div className="grid h-11 w-11 shrink-0 place-items-center rounded-xl bg-ink-700 text-fg-muted">
              <Puzzle className="h-5 w-5" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <h3 className="truncate text-base font-medium text-fg">{stage.name}</h3>
                <span className="pill bg-ink-700 text-fg-muted">v{stage.version}</span>
                {store ? (
                  <span className="pill bg-online/15 text-online">
                    <ShieldCheck className="h-3 w-3" /> Store
                  </span>
                ) : (
                  <span className="pill bg-[#e0a54a]/15 text-[#e6b45a]">Sideloaded</span>
                )}
              </div>
              <p className="mt-0.5 text-xs text-fg-muted">
                {stage.author ? `by ${stage.author}` : "Unknown author"}
                {stage.description ? ` · ${stage.description}` : ""}
              </p>
            </div>
          </div>

          {!store && (
            <div className="flex items-start gap-2.5 rounded-lg border border-rec/30 bg-rec/10 px-3 py-2.5 text-sm text-fg">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-rec" />
              <span>
                <b>Sideloaded extension.</b> This came from a file or URL you provided and has{" "}
                <b>not been reviewed by anyone</b>. Only continue if you trust where it came from.
              </span>
            </div>
          )}

          <div>
            <p className="label">
              {mode === "update" ? "This update will be allowed to:" : "This extension will be allowed to:"}
            </p>
            <PermissionList lines={stage.consent} />
          </div>

          {needsCamera && (
            <div className="rounded-lg border border-line bg-ink-900/40 p-3">
              <div className="mb-2 flex items-center gap-2">
                <Cctv className="h-4 w-4 text-fg-muted" />
                <div>
                  <p className="text-sm text-fg">Cameras</p>
                  <p className="text-[11px] text-fg-faint">
                    You decide which cameras this extension can see — the request is a default, not a rule.
                  </p>
                </div>
              </div>
              {choices.length === 0 ? (
                <p className="text-xs text-fg-faint">No cameras are available to you to grant.</p>
              ) : (
                <CameraGrantList choices={choices} selected={selected} onChange={setSelected} />
              )}
            </div>
          )}

          {error && <p className="text-sm text-rec">{error}</p>}
        </div>
      )}
    </Modal>
  );
}
