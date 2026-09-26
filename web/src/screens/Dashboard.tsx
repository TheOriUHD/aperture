import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Cctv, VideoOff } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { CameraTile } from "../components/CameraTile";
import { CameraPopout } from "../components/CameraPopout";
import { EmptyState } from "../components/ui/EmptyState";
import { OverviewSidebar } from "../components/OverviewCards";
import { MobileDashboard } from "../components/MobileDashboard";
import { useCameras, visibleCameras } from "../hooks/useCameras";
import { useMediaQuery } from "../hooks/useMediaQuery";
import type { Camera } from "../lib/types";

export function Dashboard() {
  const navigate = useNavigate();
  const { data: cameras } = useCameras();
  const [popout, setPopout] = useState<Camera | null>(null);
  const all = cameras ?? [];
  // A disabled camera is left out rather than tiled as a dead placeholder: it is
  // neither streaming nor recording. Settings -> Cameras still lists it, which is
  // where it gets switched back on.
  const list = useMemo(() => visibleCameras(cameras) ?? [], [cameras]);
  const isDesktop = useMediaQuery("(min-width: 1024px)");

  // Cameras exist but every one of them is off: say that, rather than "No cameras
  // yet", which would send the operator off to add a camera they already have.
  const empty =
    all.length === 0 ? (
      <EmptyState icon={Cctv} title="No cameras yet" hint="Add a camera to see live feeds here." />
    ) : (
      <EmptyState
        icon={VideoOff}
        title="All cameras are disabled"
        hint="Switch one on in Settings → Cameras to see live feeds."
      />
    );

  return (
    <>
      <TopBar title="Dashboard" />

      {isDesktop ? (
        // Desktop: cameras main area + UniFi-style overview sidebar.
        <div className="flex min-h-0 flex-1 flex-row">
          <div className="min-w-0 flex-1 overflow-auto p-4">
            {list.length === 0 ? (
              empty
            ) : (
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2 2xl:grid-cols-3">
                {list.map((cam) => (
                  <CameraTile key={cam.id} camera={cam} onOpen={setPopout} />
                ))}
              </div>
            )}
          </div>
          <aside className="w-[360px] shrink-0 overflow-auto border-l border-line p-4">
            <OverviewSidebar cameras={list} allCameras={all} />
          </aside>
        </div>
      ) : list.length === 0 ? (
        <div className="flex-1 overflow-auto p-4">{empty}</div>
      ) : (
        <MobileDashboard cameras={list} onOpen={setPopout} />
      )}

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
