import { useState } from "react";
import { Plus, Cctv, Circle, ChevronRight } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { EmptyState } from "../components/ui/EmptyState";
import { Spinner } from "../components/ui/Spinner";
import { CameraModal } from "../components/CameraModal";
import { useAuth } from "../lib/auth";
import { useCameras } from "../hooks/useCameras";
import type { Camera, RecordMode } from "../lib/types";

const modeLabels: Record<RecordMode, string> = {
  never: "Off",
  continuous: "24/7",
  motion: "Motion",
  scheduled: "Scheduled",
};

// CamerasManager is the camera list + editor, layout-agnostic so it can render
// both on the /cameras route and inside Settings → Cameras.
export function CamerasManager() {
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const { data: cameras, isLoading } = useCameras();

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<Camera | null>(null);

  function openAdd() {
    setEditing(null);
    setEditorOpen(true);
  }
  function openEdit(cam: Camera) {
    if (!isAdmin) return;
    setEditing(cam);
    setEditorOpen(true);
  }

  return (
    <div className="space-y-3">
      {isAdmin && (
        <div className="flex justify-end">
          <button className="btn-primary" onClick={openAdd}>
            <Plus className="h-4 w-4" />
            Add camera
          </button>
        </div>
      )}
      {isLoading ? (
        <div className="grid place-items-center py-12">
          <Spinner label="Loading cameras" />
        </div>
      ) : !cameras || cameras.length === 0 ? (
        <EmptyState
          icon={Cctv}
          title="No cameras configured"
          hint={isAdmin ? "Add an RTSP camera to begin." : "Ask an administrator to add cameras."}
        />
      ) : (
        <div className="space-y-2">
          {cameras.map((cam) => (
            <button
              key={cam.id}
              onClick={() => openEdit(cam)}
              disabled={!isAdmin}
              className="w-full text-left card px-4 py-3 flex items-center gap-4 transition-colors enabled:hover:border-line-strong enabled:hover:bg-ink-750 disabled:cursor-default group"
            >
              <div className="h-9 w-9 rounded-md bg-ink-700 grid place-items-center shrink-0">
                <Cctv className="h-4 w-4 text-fg-muted" />
              </div>
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium text-fg truncate">{cam.name}</p>
                {/* Login masked for admins; viewers are sent no link at all. */}
                {cam.rtspMainUrl && (
                  <p className="text-xs text-fg-faint truncate font-mono">{cam.rtspMainUrl}</p>
                )}
              </div>
              <span className="pill bg-ink-700 text-fg-muted">{modeLabels[cam.recordMode]}</span>
              <span className="hidden sm:flex items-center gap-1.5 text-xs text-fg-muted w-20">
                <Circle
                  className={
                    cam.enabled
                      ? "h-2 w-2 fill-online text-online"
                      : "h-2 w-2 fill-fg-faint text-fg-faint"
                  }
                />
                {cam.enabled ? "Enabled" : "Disabled"}
              </span>
              {isAdmin && (
                <ChevronRight className="h-4 w-4 text-fg-faint group-hover:text-fg-muted shrink-0" />
              )}
            </button>
          ))}
        </div>
      )}
      <CameraModal open={editorOpen} camera={editing} onClose={() => setEditorOpen(false)} />
    </div>
  );
}

// Cameras is the standalone route (kept for deep links) — a header + the manager.
export function Cameras() {
  return (
    <>
      <TopBar title="Cameras" />
      <div className="flex-1 overflow-auto p-4">
        <div className="max-w-4xl mx-auto">
          <CamerasManager />
        </div>
      </div>
    </>
  );
}
