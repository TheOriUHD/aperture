import { useEffect, type ReactNode } from "react";
import { X } from "lucide-react";
import { useScrollLock } from "../../hooks/useScrollLock";

export function Modal({
  open,
  onClose,
  title,
  children,
  footer,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  footer?: ReactNode;
}) {
  useScrollLock(open);
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 grid place-items-center p-4">
      <div className="absolute inset-0 bg-black/60 animate-fade-in" onClick={onClose} />
      <div className="relative flex max-h-[85dvh] w-full max-w-md flex-col card shadow-pop animate-fade-in">
        <div className="flex items-center justify-between px-5 h-14 border-b border-line shrink-0">
          <h2 className="text-base font-medium text-fg">{title}</h2>
          <button className="btn-icon" onClick={onClose} aria-label="Close">
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="overflow-y-auto overflow-x-hidden p-5">{children}</div>
        {footer && (
          <div className="flex justify-end gap-2 px-5 py-4 border-t border-line shrink-0">{footer}</div>
        )}
      </div>
    </div>
  );
}
