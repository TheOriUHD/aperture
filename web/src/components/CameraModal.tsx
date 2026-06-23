import { useEffect, useState } from "react";
import { Trash2, CalendarClock } from "lucide-react";
import { Modal } from "./ui/Modal";
import { ConfirmDialog } from "./ui/ConfirmDialog";
import { Switch } from "./ui/Switch";
import { ScheduleModal } from "./ScheduleModal";
import { useCreateCamera, useUpdateCamera, useDeleteCamera } from "../hooks/useCameras";
import type { Camera, RecordMode } from "../lib/types";

export function CameraModal({
  open,
  camera,
  onClose,
}: {
  open: boolean;
  camera: Camera | null; // null = add
  onClose: () => void;
}) {
  const isEdit = !!camera;
  const create = useCreateCamera();
  const update = useUpdateCamera();
  const del = useDeleteCamera();

  const [name, setName] = useState("");
  const [main, setMain] = useState("");
  const [sub, setSub] = useState("");
  const [mode, setMode] = useState<RecordMode>("continuous");
  const [retention, setRetention] = useState(7);
  const [enabled, setEnabled] = useState(true);
  const [error, setError] = useState("");
  const [confirmDel, setConfirmDel] = useState(false);
  const [scheduleOpen, setScheduleOpen] = useState(false);

  useEffect(() => {
    if (!open) return;
    setError("");
    setConfirmDel(false);
    if (camera) {
      setName(camera.name);
      setMain(camera.rtspMainUrl);
      setSub(camera.rtspSubUrl);
      setMode(camera.recordMode);
      setRetention(camera.retentionDays);
      setEnabled(camera.enabled);
    } else {
      setName("");
      setMain("");
      setSub("");
      setMode("continuous");
      setRetention(7);
      setEnabled(true);
    }
  }, [open, camera]);

  const busy = create.isPending || update.isPending;

  async function save() {
    if (!name.trim() || !main.trim()) {
      setError("Name and main RTSP URL are required.");
      return;
    }
    const input = {
      name: name.trim(),
      rtspMainUrl: main.trim(),
      rtspSubUrl: sub.trim(),
      recordMode: mode,
      retentionDays: retention,
      enabled,
    };
    try {
      if (camera) await update.mutateAsync({ id: camera.id, ...input });
      else await create.mutateAsync(input);
      onClose();
    } catch {
      setError("Could not save the camera.");
    }
  }

  async function doDelete() {
    if (!camera) return;
    try {
      await del.mutateAsync(camera.id);
      setConfirmDel(false);
      onClose();
    } catch {
      setError("Could not delete the camera.");
      setConfirmDel(false);
    }
  }

  return (
    <>
      <Modal
        open={open}
        onClose={onClose}
        title={isEdit ? "Edit camera" : "Add camera"}
        footer={
          <div className="flex items-center justify-between w-full">
            <div>
              {isEdit && (
                <button
                  className="btn-ghost !text-rec !border-rec/30 hover:!bg-rec/10"
                  onClick={() => setConfirmDel(true)}
                >
                  <Trash2 className="h-4 w-4" />
                  Delete
                </button>
              )}
            </div>
            <div className="flex gap-2">
              <button className="btn-ghost" onClick={onClose}>
                Cancel
              </button>
              <button className="btn-primary" onClick={save} disabled={busy}>
                {busy ? "Saving…" : isEdit ? "Save changes" : "Add camera"}
              </button>
            </div>
          </div>
        }
      >
        <div className="space-y-4">
          <div>
            <label className="label">Name</label>
            <input
              className="input"
              placeholder="Front Door"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
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
            <label className="label">Sub stream (optional, used for motion)</label>
            <input
              className="input font-mono text-xs"
              placeholder="rtsp://user:pass@192.168.1.50:554/stream2"
              value={sub}
              onChange={(e) => setSub(e.target.value)}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="label">Recording mode</label>
              <select
                className="input"
                value={mode}
                onChange={(e) => setMode(e.target.value as RecordMode)}
              >
                <option value="continuous">24/7 continuous</option>
                <option value="motion">Motion only</option>
                <option value="scheduled">Scheduled</option>
                <option value="never">Off</option>
              </select>
            </div>
            <div>
              <label className="label">Retention (days)</label>
              <input
                type="number"
                min={1}
                className="input"
                value={retention}
                onChange={(e) => setRetention(Math.max(1, Number(e.target.value) || 1))}
              />
            </div>
          </div>
          {mode === "scheduled" &&
            (isEdit ? (
              <button type="button" className="btn-subtle w-full" onClick={() => setScheduleOpen(true)}>
                <CalendarClock className="h-4 w-4" />
                Edit recording schedule
              </button>
            ) : (
              <p className="text-xs text-fg-faint">
                Save the camera first, then open it again to set its recording schedule.
              </p>
            ))}

          {isEdit && (
            <div className="flex items-center justify-between pt-1">
              <div>
                <span className="text-sm text-fg">Enabled</span>
                <p className="text-xs text-fg-faint">Stream and record this camera</p>
              </div>
              <Switch checked={enabled} onChange={setEnabled} label="Enabled" />
            </div>
          )}
          {error && <p className="text-sm text-rec">{error}</p>}
        </div>
      </Modal>

      <ConfirmDialog
        open={confirmDel}
        danger
        busy={del.isPending}
        title="Delete camera"
        message={`"${camera?.name}" and its recordings will be permanently removed. This can't be undone.`}
        confirmLabel="Delete camera"
        onConfirm={doDelete}
        onCancel={() => setConfirmDel(false)}
      />

      {camera && (
        <ScheduleModal
          open={scheduleOpen}
          cameraId={camera.id}
          cameraName={camera.name}
          onClose={() => setScheduleOpen(false)}
        />
      )}
    </>
  );
}
