import { useState } from "react";
import { Plus, Cctv, Trash2, Circle } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { EmptyState } from "../components/ui/EmptyState";
import { Spinner } from "../components/ui/Spinner";
import { Modal } from "../components/ui/Modal";
import { useAuth } from "../lib/auth";
import {
  useCameras,
  useCreateCamera,
  useDeleteCamera,
  type CameraInput,
} from "../hooks/useCameras";
import type { RecordMode } from "../lib/types";

const modeLabels: Record<RecordMode, string> = {
  never: "Off",
  continuous: "24/7",
  motion: "Motion",
  scheduled: "Scheduled",
};

export function Cameras() {
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const { data: cameras, isLoading } = useCameras();
  const create = useCreateCamera();
  const del = useDeleteCamera();
  const [adding, setAdding] = useState(false);

  return (
    <>
      <TopBar
        title="Cameras"
        right={
          isAdmin && (
            <button className="btn-primary" onClick={() => setAdding(true)}>
              <Plus className="h-4 w-4" />
              Add camera
            </button>
          )
        }
      />
      <div className="flex-1 overflow-auto p-4">
        {isLoading ? (
          <div className="h-full grid place-items-center">
            <Spinner label="Loading cameras" />
          </div>
        ) : !cameras || cameras.length === 0 ? (
          <EmptyState
            icon={Cctv}
            title="No cameras configured"
            hint={isAdmin ? "Add an RTSP camera to begin." : "Ask an administrator to add cameras."}
            action={
              isAdmin && (
                <button className="btn-primary" onClick={() => setAdding(true)}>
                  <Plus className="h-4 w-4" />
                  Add camera
                </button>
              )
            }
          />
        ) : (
          <div className="max-w-4xl mx-auto space-y-2">
            {cameras.map((cam) => (
              <div
                key={cam.id}
                className="card px-4 py-3 flex items-center gap-4 hover:border-line-strong transition-colors"
              >
                <div className="h-9 w-9 rounded-md bg-ink-700 grid place-items-center shrink-0">
                  <Cctv className="h-4 w-4 text-fg-muted" />
                </div>
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-medium text-fg truncate">{cam.name}</p>
                  <p className="text-xs text-fg-faint truncate font-mono">{cam.rtspMainUrl}</p>
                </div>
                <span className="pill bg-ink-700 text-fg-muted">{modeLabels[cam.recordMode]}</span>
                <span className="flex items-center gap-1.5 text-xs text-fg-muted w-20">
                  <Circle
                    className={cam.enabled ? "h-2 w-2 fill-online text-online" : "h-2 w-2 fill-fg-faint text-fg-faint"}
                  />
                  {cam.enabled ? "Enabled" : "Disabled"}
                </span>
                {isAdmin && (
                  <button
                    className="btn-icon text-fg-faint hover:text-rec"
                    aria-label="Delete camera"
                    onClick={() => {
                      if (confirm(`Delete camera "${cam.name}"?`)) del.mutate(cam.id);
                    }}
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      <AddCameraModal
        open={adding}
        onClose={() => setAdding(false)}
        busy={create.isPending}
        onSubmit={async (input) => {
          await create.mutateAsync(input);
          setAdding(false);
        }}
      />
    </>
  );
}

function AddCameraModal({
  open,
  onClose,
  onSubmit,
  busy,
}: {
  open: boolean;
  onClose: () => void;
  onSubmit: (input: CameraInput) => Promise<void>;
  busy: boolean;
}) {
  const [name, setName] = useState("");
  const [main, setMain] = useState("");
  const [sub, setSub] = useState("");
  const [mode, setMode] = useState<RecordMode>("continuous");
  const [error, setError] = useState("");

  function reset() {
    setName("");
    setMain("");
    setSub("");
    setMode("continuous");
    setError("");
  }

  return (
    <Modal
      open={open}
      onClose={() => {
        reset();
        onClose();
      }}
      title="Add camera"
      footer={
        <>
          <button
            className="btn-ghost"
            onClick={() => {
              reset();
              onClose();
            }}
          >
            Cancel
          </button>
          <button
            className="btn-primary"
            disabled={busy}
            onClick={async () => {
              if (!name.trim() || !main.trim()) {
                setError("Name and main RTSP URL are required.");
                return;
              }
              try {
                await onSubmit({ name, rtspMainUrl: main, rtspSubUrl: sub, recordMode: mode });
                reset();
              } catch {
                setError("Could not add the camera.");
              }
            }}
          >
            {busy ? "Adding…" : "Add camera"}
          </button>
        </>
      }
    >
      <div className="space-y-4">
        <div>
          <label className="label">Name</label>
          <input className="input" placeholder="Front Door" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div>
          <label className="label">Main stream (RTSP URL)</label>
          <input
            className="input font-mono text-xs"
            placeholder="rtsp://user:pass@192.168.1.50:554/stream1"
            value={main}
            onChange={(e) => setMain(e.target.value)}
          />
        </div>
        <div>
          <label className="label">Sub stream (optional, for motion)</label>
          <input
            className="input font-mono text-xs"
            placeholder="rtsp://user:pass@192.168.1.50:554/stream2"
            value={sub}
            onChange={(e) => setSub(e.target.value)}
          />
        </div>
        <div>
          <label className="label">Recording mode</label>
          <select className="input" value={mode} onChange={(e) => setMode(e.target.value as RecordMode)}>
            <option value="continuous">24/7 continuous</option>
            <option value="motion">Motion only</option>
            <option value="scheduled">Scheduled</option>
            <option value="never">Off</option>
          </select>
        </div>
        {error && <p className="text-sm text-rec">{error}</p>}
      </div>
    </Modal>
  );
}
