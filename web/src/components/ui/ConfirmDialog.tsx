import { useId } from "react";
import { AlertTriangle } from "lucide-react";
import clsx from "clsx";
import { useScrollLock } from "../../hooks/useScrollLock";
import { useEscapeStack } from "../../lib/escapeStack";

export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  danger = false,
  busy = false,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
  busy?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  useScrollLock(open);
  useEscapeStack(open, onCancel); // topmost overlay only (see lib/escapeStack)
  const titleId = useId();
  const bodyId = useId();

  if (!open) return null;
  // !m-0: dialogs render inline, so a parent's space-y-* would otherwise add a
  // top margin to the fixed overlay and leave a strip of the header undimmed.
  return (
    <div className="fixed inset-0 z-[60] !m-0 grid place-items-center p-4">
      <div className="absolute inset-0 bg-black/65 animate-fade-in" onClick={onCancel} />
      <div
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={bodyId}
        className="relative w-full max-w-[400px] rounded-lg border border-line bg-ink-800 shadow-pop animate-pop-in"
      >
        <div className="flex gap-3.5 px-5 pb-5 pt-5">
          <div
            className={clsx(
              "grid h-9 w-9 shrink-0 place-items-center rounded-md",
              danger ? "bg-danger/15 text-danger" : "bg-brand/15 text-brand",
            )}
          >
            <AlertTriangle className="h-[18px] w-[18px]" />
          </div>
          <div className="min-w-0 pt-0.5">
            <h3 id={titleId} className="text-sm font-semibold text-fg">
              {title}
            </h3>
            <p id={bodyId} className="mt-1.5 text-sm leading-relaxed text-fg-muted">
              {message}
            </p>
          </div>
        </div>
        <div className="flex items-center justify-end gap-2 border-t border-line px-5 py-3">
          <button className="btn-ghost" onClick={onCancel} disabled={busy}>
            {cancelLabel}
          </button>
          <button className={danger ? "btn-danger" : "btn-primary"} onClick={onConfirm} disabled={busy} autoFocus>
            {busy ? "Working…" : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
