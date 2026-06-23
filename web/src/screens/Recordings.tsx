import { useState } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { Timeline } from "../components/Timeline";
import { useCameras } from "../hooks/useCameras";

function startOfDay(d: Date) {
  const x = new Date(d);
  x.setHours(0, 0, 0, 0);
  return x;
}

export function Recordings() {
  const { data: cameras } = useCameras();
  const [day, setDay] = useState(() => startOfDay(new Date()));
  const [cameraId, setCameraId] = useState<string | null>(null);

  const selected = cameras?.find((c) => c.id === cameraId) ?? cameras?.[0] ?? null;
  const dayStart = day.getTime();

  return (
    <>
      <TopBar
        title="Recordings"
        right={
          cameras && cameras.length > 0 ? (
            <select
              className="input h-8 w-44"
              value={selected?.id ?? ""}
              onChange={(e) => setCameraId(e.target.value)}
            >
              {cameras.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          ) : undefined
        }
      />
      <div className="flex-1 overflow-auto p-4">
        <div className="max-w-5xl mx-auto space-y-4">
          {/* Player surface */}
          <div className="aspect-video rounded-lg bg-black border border-line grid place-items-center">
            <p className="text-sm text-fg-faint">
              {selected ? "Select a moment on the timeline" : "No cameras to play back"}
            </p>
          </div>

          {/* Day navigation */}
          <div className="flex items-center gap-3">
            <button className="btn-icon" onClick={() => setDay(new Date(dayStart - 86400000))}>
              <ChevronLeft className="h-4 w-4" />
            </button>
            <span className="text-sm font-medium text-fg">
              {day.toLocaleDateString(undefined, {
                weekday: "short",
                month: "short",
                day: "numeric",
              })}
            </span>
            <button
              className="btn-icon"
              onClick={() => setDay(new Date(dayStart + 86400000))}
              disabled={dayStart >= startOfDay(new Date()).getTime()}
            >
              <ChevronRight className="h-4 w-4" />
            </button>
            <div className="ml-auto flex items-center gap-3 text-[11px] text-fg-muted">
              <span className="flex items-center gap-1.5">
                <span className="h-2.5 w-2.5 rounded-sm bg-brand-deep" /> Continuous
              </span>
              <span className="flex items-center gap-1.5">
                <span className="h-2.5 w-2.5 rounded-sm bg-motion" /> Motion
              </span>
            </div>
          </div>

          {/* The signature timeline */}
          <Timeline dayStart={dayStart} segments={[]} events={[]} />
        </div>
      </div>
    </>
  );
}
