import { useEffect, useState } from "react";
import { Search, Lock, Cctv } from "lucide-react";
import type { Camera } from "../lib/types";

export function CameraRail({
  cameras,
  selectedId,
  onSelect,
}: {
  cameras: Camera[];
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  const [q, setQ] = useState("");
  const filtered = cameras.filter((c) => c.name.toLowerCase().includes(q.toLowerCase()));

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
        {filtered.map((c) => (
          <button
            key={c.id}
            onClick={() => onSelect(c.id)}
            className={`block w-full rounded-lg overflow-hidden border transition-colors ${
              c.id === selectedId
                ? "border-brand ring-1 ring-brand"
                : "border-line hover:border-line-strong"
            }`}
          >
            <SnapshotThumb cameraId={c.id} enabled={c.enabled} />
            <div className="flex items-center gap-1.5 px-2 py-1.5">
              <Lock className="h-3 w-3 text-fg-faint shrink-0" />
              <span
                className={`text-xs truncate ${c.id === selectedId ? "text-brand font-medium" : "text-fg-muted"}`}
              >
                {c.name}
              </span>
            </div>
          </button>
        ))}
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
          src={`/api/cameras/${cameraId}/snapshot.jpg?t=${tick}`}
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
