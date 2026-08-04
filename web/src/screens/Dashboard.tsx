import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Cctv } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { CameraTile } from "../components/CameraTile";
import { CameraPopout } from "../components/CameraPopout";
import { EmptyState } from "../components/ui/EmptyState";
import { OverviewSidebar } from "../components/OverviewCards";
import { MobileDashboard } from "../components/MobileDashboard";
import { useCameras } from "../hooks/useCameras";
import { useMediaQuery } from "../hooks/useMediaQuery";
import type { Camera } from "../lib/types";

export function Dashboard() {
  const navigate = useNavigate();
  const { data: cameras } = useCameras();
  const [popout, setPopout] = useState<Camera | null>(null);
  const list = cameras ?? [];
  const isDesktop = useMediaQuery("(min-width: 1024px)");

  return (
    <>
      <TopBar title="Dashboard" />

      {isDesktop ? (
        // Desktop: cameras main area + UniFi-style overview sidebar.
        <div className="flex min-h-0 flex-1 flex-row">
          <div className="min-w-0 flex-1 overflow-auto p-4">
            {list.length === 0 ? (
              <EmptyState icon={Cctv} title="No cameras yet" hint="Add a camera to see live feeds here." />
            ) : (
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2 2xl:grid-cols-3">
                {list.map((cam) => (
                  <CameraTile key={cam.id} camera={cam} onOpen={setPopout} />
                ))}
              </div>
            )}
          </div>
          <aside className="w-[360px] shrink-0 overflow-auto border-l border-line p-4">
            <OverviewSidebar cameras={list} />
          </aside>
        </div>
      ) : list.length === 0 ? (
        <div className="flex-1 overflow-auto p-4">
          <EmptyState icon={Cctv} title="No cameras yet" hint="Add a camera to see live feeds here." />
        </div>
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
