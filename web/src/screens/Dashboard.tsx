import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { HardDrive, Cctv, Video, Activity } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { CameraTile } from "../components/CameraTile";
import { CameraPopout } from "../components/CameraPopout";
import { EmptyState } from "../components/ui/EmptyState";
import { useCameras } from "../hooks/useCameras";
import { useStorage } from "../hooks/useStorage";
import { api } from "../lib/api";
import { formatBytes } from "../lib/format";
import type { Camera, MotionEvent } from "../lib/types";

function startOfToday() {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  return d.getTime();
}

export function Dashboard() {
  const navigate = useNavigate();
  const { data: cameras } = useCameras();
  const { data: storage } = useStorage();
  const { data: todayEvents } = useQuery({
    queryKey: ["events-today"],
    queryFn: () =>
      api.get<MotionEvent[]>(`/api/events?from=${startOfToday()}&to=${Date.now()}&limit=500`),
    refetchInterval: 30000,
  });
  const [popout, setPopout] = useState<Camera | null>(null);
  const list = cameras ?? [];
  const online = list.filter((c) => c.enabled).length;
  const recording = list.filter((c) => c.enabled && c.recordMode !== "never").length;

  return (
    <>
      <TopBar title="Dashboard" />
      <div className="flex-1 overflow-auto p-4">
        <div className="max-w-[1600px] mx-auto space-y-4">
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
            <Metric
              icon={HardDrive}
              label="Recordings"
              value={storage ? formatBytes(storage.recordedBytes) : "…"}
              sub={storage ? `${formatBytes(storage.diskFreeBytes)} free` : ""}
            />
            <Metric icon={Cctv} label="Cameras online" value={`${online}`} sub={`of ${list.length}`} />
            <Metric icon={Video} label="Recording" value={`${recording}`} sub="cameras" />
            <Metric
              icon={Activity}
              label="Events today"
              value={`${todayEvents?.length ?? 0}`}
              sub="motion"
            />
          </div>

          {list.length === 0 ? (
            <EmptyState icon={Cctv} title="No cameras yet" hint="Add a camera to see live feeds here." />
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 2xl:grid-cols-3 gap-3">
              {list.map((cam) => (
                <CameraTile key={cam.id} camera={cam} onOpen={setPopout} />
              ))}
            </div>
          )}
        </div>
      </div>

      <CameraPopout
        camera={popout}
        onClose={() => setPopout(null)}
        onOpenPlayback={(id) => {
          setPopout(null);
          navigate(`/playback?camera=${id}`);
        }}
      />
    </>
  );
}

function Metric({
  icon: Icon,
  label,
  value,
  sub,
}: {
  icon: LucideIcon;
  label: string;
  value: string;
  sub: string;
}) {
  return (
    <div className="panel">
      <div className="flex items-center gap-2 text-fg-muted mb-3">
        <Icon className="h-4 w-4" />
        <span className="text-xs">{label}</span>
      </div>
      <div className="flex items-baseline gap-1.5">
        <span className="text-2xl font-medium text-fg">{value}</span>
        <span className="text-xs text-fg-faint">{sub}</span>
      </div>
    </div>
  );
}
