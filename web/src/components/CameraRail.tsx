import { useEffect, useState } from "react";
import { Search, Lock, Cctv } from "lucide-react";
import type { Camera } from "../lib/types";

export function CameraRail({
  cameras,
  selectedId,
  onSelect,
  multiIds,
}: {
  cameras: Camera[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  // When set, the rail is in multi-view mode: it shows ONLY these cameras, all
  // highlighted, and selecting does nothing (the grid is driven by the modal).
  multiIds?: string[];
}) {
  const [q, setQ] = useState("");
  const multi = !!multiIds?.length;
  const base = multi ? cameras.filter((c) => multiIds!.includes(c.id)) : cameras;
  const filtered = base.filter((c) => c.name.toLowerCase().includes(q.toLowerCase()));

  return (
    <div className="frame flex w-[212px] shrink-0 flex-col overflow-hidden">
      <div className="px-3 pb-2 pt-3">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-faint" />
          <input
            className="input pl-8"
            placeholder="Search"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </div>
      </div>
      <div className="flex-1 space-y-1 overflow-auto px-3 pb-3">
        {filtered.map((c) => {
          const active = c.id === selectedId;
          // The selected camera sits on a raised tile with its name in a blue
          // label; in multi-view every camera in the grid reads as selected.
          const marked = multi || active;
          return (
            <button
              key={c.id}
              onClick={() => {
                if (!multi) onSelect(c.id);
              }}
              aria-pressed={marked}
              className={`group block w-full rounded-lg text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/60 ${
                multi ? "cursor-default" : ""
              }`}
            >
              <div
                className={`rounded-lg p-1 transition-colors ${
                  marked ? "bg-ink-650" : "group-hover:bg-white/[0.05]"
                }`}
              >
                <div className="overflow-hidden rounded-md">
                  <SnapshotThumb cameraId={c.id} enabled={c.enabled} />
                </div>
              </div>
              <div className="flex justify-center px-1 pb-1.5 pt-1">
                {marked ? (
                  <span className="inline-flex max-w-full items-center gap-1 rounded-md bg-brand px-1.5 py-[3px] text-xs font-medium text-white">
                    <Lock className="h-3 w-3 shrink-0" />
                    <span className="truncate">{c.name}</span>
                  </span>
                ) : (
                  <span className="inline-flex max-w-full items-center gap-1 px-1.5 py-[3px] text-xs text-fg group-hover:text-fg">
                    <Lock className="h-3 w-3 shrink-0 text-fg-muted" />
                    <span className="truncate">{c.name}</span>
                  </span>
                )}
              </div>
            </button>
          );
        })}
        {filtered.length === 0 && (
          <p className="px-1 py-6 text-center text-xs text-fg-faint">
            {q ? `No cameras match “${q}”` : "No cameras"}
          </p>
        )}
      </div>
    </div>
  );
}

function SnapshotThumb({ cameraId, enabled }: { cameraId: string; enabled: boolean }) {
  const [tick, setTick] = useState(0);
  const [ok, setOk] = useState(true);
  useEffect(() => {
    if (!enabled) return;
    const i = setInterval(() => setTick((t) => t + 1), 5000);
    return () => clearInterval(i);
  }, [enabled]);

  return (
    <div className="relative aspect-video bg-ink-950">
      {enabled && ok ? (
        <img
          src={`/api/v1/cameras/${cameraId}/snapshot.jpg?t=${tick}`}
          alt=""
          className="h-full w-full object-cover"
          onError={() => setOk(false)}
        />
      ) : (
        <div className="absolute inset-0 grid place-items-center">
          <Cctv className="h-5 w-5 text-fg-faint" />
        </div>
      )}
    </div>
  );
}
