import { Puzzle, AlertTriangle, ShieldCheck } from "lucide-react";
import { Modal } from "../components/ui/Modal";
import { PermissionList } from "./PermissionList";
import type { StageView } from "./types";

// The consent decision, as a modal over the manager (a decision moment, not a
// destination). Identity on top; permissions in plain language, weighted so the
// consequential grants stand out; the sideload warning is unmissable for non-store
// installs; the buttons are weighted honestly — Install is primary, Cancel is a
// plain readable choice, neither styled to nudge.

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
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const open = !!stage;
  const store = stage?.source === "store";
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
          <button className="btn-primary" onClick={onConfirm} disabled={busy}>
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

          {error && <p className="text-sm text-rec">{error}</p>}
        </div>
      )}
    </Modal>
  );
}
