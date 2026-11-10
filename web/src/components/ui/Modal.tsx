import { useId, type ReactNode } from "react";
import { X } from "lucide-react";
import clsx from "clsx";
import { useScrollLock } from "../../hooks/useScrollLock";
import { useEscapeStack } from "../../lib/escapeStack";

const SIZES = { md: "max-w-md", lg: "max-w-2xl", xl: "max-w-5xl", "2xl": "max-w-6xl" } as const;

export function Modal({
  open,
  onClose,
  title,
  children,
  footer,
  size = "md",
  bodyClass,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  footer?: ReactNode;
  size?: keyof typeof SIZES;
  /** Overrides the default padded, scrolling body — e.g. a flush flex body for a
   * full-height editor. Defaults to `overflow-y-auto overflow-x-hidden p-5`. */
  bodyClass?: string;
}) {
  useScrollLock(open);
  useEscapeStack(open, onClose); // topmost overlay only (see lib/escapeStack)

  const titleId = useId();

  if (!open) return null;
  // !m-0: dialogs render inline, so a parent's space-y-* would otherwise add a
  // top margin to the fixed overlay and leave a strip of the header undimmed.
  return (
    <div className="fixed inset-0 z-50 !m-0 grid place-items-center p-4">
      <div className="absolute inset-0 bg-black/60 animate-fade-in" onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={clsx(
          "relative flex max-h-[85dvh] w-full flex-col rounded-lg border border-line bg-ink-800 shadow-pop animate-pop-in",
          SIZES[size],
        )}
      >
        <div className="flex h-12 shrink-0 items-center justify-between gap-3 border-b border-line pl-5 pr-3">
          <h2 id={titleId} className="min-w-0 truncate text-sm font-semibold text-fg">
            {title}
          </h2>
          <button className="btn-icon shrink-0" onClick={onClose} aria-label="Close">
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className={bodyClass ?? "overflow-y-auto overflow-x-hidden p-5"}>{children}</div>
        {footer && (
          <div className="flex shrink-0 flex-wrap items-center justify-end gap-2 border-t border-line px-5 py-3">
            {footer}
          </div>
        )}
      </div>
    </div>
  );
}
