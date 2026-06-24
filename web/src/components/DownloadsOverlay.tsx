import { Download, Check, AlertCircle, X } from "lucide-react";
import { useDownloads } from "../lib/downloads";

export function DownloadsOverlay() {
  const { items, dismiss } = useDownloads();
  if (items.length === 0) return null;
  return (
    <div className="fixed bottom-4 right-4 z-[60] flex flex-col gap-2 w-72 pointer-events-none">
      {items.map((it) => (
        <div key={it.id} className="card shadow-pop p-3 animate-fade-in pointer-events-auto">
          <div className="flex items-center gap-2">
            {it.status === "done" ? (
              <Check className="h-4 w-4 text-online shrink-0" />
            ) : it.status === "error" ? (
              <AlertCircle className="h-4 w-4 text-rec shrink-0" />
            ) : (
              <Download className="h-4 w-4 text-brand shrink-0" />
            )}
            <span className="text-xs text-fg flex-1 truncate">
              {it.status === "error"
                ? "Download failed"
                : it.status === "done"
                  ? `Saved · ${it.label}`
                  : `Downloading ${it.label}`}
            </span>
            <button
              className="btn-icon h-6 w-6 shrink-0"
              onClick={() => dismiss(it.id)}
              aria-label="Dismiss"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
          {it.status === "downloading" && (
            <div className="mt-2 h-1 rounded-full bg-ink-700 overflow-hidden">
              <div
                className={`h-full bg-brand transition-[width] ${it.progress < 0 ? "w-1/3 animate-pulse" : ""}`}
                style={it.progress >= 0 ? { width: `${Math.round(it.progress * 100)}%` } : undefined}
              />
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
