import { Download, Check, AlertCircle, X } from "lucide-react";
import clsx from "clsx";
import { useDownloads } from "../lib/downloads";

// DownloadsOverlay stacks download toasts in the bottom-right corner (above the
// tab bar on a phone): what is downloading, its progress, and a dismiss button.
export function DownloadsOverlay() {
  const { items, dismiss } = useDownloads();
  if (items.length === 0) return null;
  return (
    <div
      className="pointer-events-none fixed inset-x-3 bottom-[calc(4rem+env(safe-area-inset-bottom))] z-[60] flex flex-col gap-2 lg:inset-x-auto lg:bottom-4 lg:right-4 lg:w-80"
      aria-live="polite"
    >
      {items.map((it) => (
        <div
          key={it.id}
          className="pointer-events-auto animate-pop-in rounded-lg border border-line bg-ink-800 px-3 py-2.5 shadow-pop"
        >
          <div className="flex items-center gap-2.5">
            <span
              className={clsx(
                "grid h-7 w-7 shrink-0 place-items-center rounded-md",
                it.status === "done"
                  ? "bg-online/15 text-online"
                  : it.status === "error"
                    ? "bg-danger/15 text-danger"
                    : "bg-brand/15 text-brand",
              )}
            >
              {it.status === "done" ? (
                <Check className="h-4 w-4" />
              ) : it.status === "error" ? (
                <AlertCircle className="h-4 w-4" />
              ) : (
                <Download className="h-4 w-4" />
              )}
            </span>
            <div className="min-w-0 flex-1">
              <p className="truncate text-[13px] font-medium text-fg">
                {it.status === "error" ? "Download failed" : it.status === "done" ? "Saved" : "Downloading"}
              </p>
              <p className="truncate text-xs text-fg-faint">
                {it.label}
                {it.status === "downloading" && it.progress >= 0 && (
                  <span className="tabular-nums"> · {Math.round(it.progress * 100)}%</span>
                )}
              </p>
            </div>
            <button className="btn-icon h-7 w-7 shrink-0" onClick={() => dismiss(it.id)} aria-label="Dismiss">
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
          {it.status === "downloading" && (
            <div className="mt-2.5 h-1 overflow-hidden rounded-full bg-white/[0.08]">
              <div
                className={clsx(
                  "h-full rounded-full bg-brand transition-[width]",
                  it.progress < 0 && "w-1/3 animate-pulse",
                )}
                style={it.progress >= 0 ? { width: `${Math.round(it.progress * 100)}%` } : undefined}
              />
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
