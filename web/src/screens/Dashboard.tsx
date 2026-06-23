import { HardDrive, Cctv, Video, Activity } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { CameraTile } from "../components/CameraTile";
import { useCameras } from "../hooks/useCameras";

export function Dashboard() {
  const { data: cameras } = useCameras();
  const list = cameras ?? [];
  const online = list.filter((c) => c.enabled).length;
  const recording = list.filter((c) => c.enabled && c.recordMode !== "never").length;

  return (
    <>
      <TopBar title="Dashboard" />
      <div className="flex-1 overflow-auto p-4">
        <div className="max-w-6xl mx-auto space-y-4">
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
            <Metric icon={HardDrive} label="Storage used" value="1.4 TB" sub="of 4.0 TB" />
            <Metric icon={Cctv} label="Cameras online" value={`${online}`} sub={`of ${list.length}`} />
            <Metric icon={Video} label="Recording" value={`${recording}`} sub="cameras" />
            <Metric icon={Activity} label="Events today" value="0" sub="motion" />
          </div>

          <div>
            <h2 className="text-sm font-medium text-fg-muted mb-2.5">Live</h2>
            {list.length === 0 ? (
              <div className="panel text-sm text-fg-faint text-center py-10">
                No cameras yet. Add one under Cameras.
              </div>
            ) : (
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
                {list.slice(0, 6).map((cam) => (
                  <CameraTile key={cam.id} camera={cam} />
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
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
