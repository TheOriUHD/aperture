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
    <div className="w-[200px] shrink-0 border-r border-line flex flex-col bg-ink-900">
      <div className="p-2.5 border-b border-line">
        <div className="relative">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-fg-faint" />
          <input
            className="input h-8 pl-8 text-xs"
            placeholder="Search"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </div>
      </div>
      <div className="flex-1 overflow-auto p-2 space-y-2">
        {filtered.map((c) => {
          const active = c.id === selectedId;
          return (
            <button
              key={c.id}
              onClick={() => {
                if (!multi) onSelect(c.id);
              }}
              className={`block w-full overflow-hidden rounded-lg border transition-colors ${
                multi
                  ? "cursor-default border-brand/50"
                  : active
                    ? "border-brand ring-1 ring-brand"
                    : "border-line hover:border-line-strong"
              }`}
            >
              <SnapshotThumb cameraId={c.id} enabled={c.enabled} />
              {multi ? (
                <div className="flex justify-center px-2 py-1.5">
                  <span className="rounded-md bg-brand px-2 py-0.5 text-xs font-medium text-white">{c.name}</span>
                </div>
              ) : (
                <div className="flex items-center gap-1.5 px-2 py-1.5">
                  <Lock className="h-3 w-3 shrink-0 text-fg-faint" />
                  <span className={`truncate text-xs ${active ? "font-medium text-brand" : "text-fg-muted"}`}>
                    {c.name}
                  </span>
                </div>
              )}
            </button>
          );
        })}
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
    <div className="relative aspect-video bg-ink-800">
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
