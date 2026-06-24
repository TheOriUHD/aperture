import { useEffect, useState } from "react";
import { Trash2, CalendarClock } from "lucide-react";
import { Modal } from "./ui/Modal";
import { ConfirmDialog } from "./ui/ConfirmDialog";
import { Switch } from "./ui/Switch";
import { ScheduleModal } from "./ScheduleModal";
import { useCreateCamera, useUpdateCamera, useDeleteCamera } from "../hooks/useCameras";
import type { Camera, RecordMode } from "../lib/types";

const SENS: { v: number; label: string }[] = [
  { v: 0.06, label: "Low" },
  { v: 0.03, label: "Medium-low" },
  { v: 0.02, label: "Medium" },
  { v: 0.012, label: "High" },
  { v: 0.006, label: "Very high" },
];
function nearestSens(v: number): number {
  return SENS.reduce((a, b) => (Math.abs(b.v - v) < Math.abs(a.v - v) ? b : a), SENS[2]).v;
}

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
  const [sensitivity, setSensitivity] = useState(0.02);
  const [preroll, setPreroll] = useState(5);
  const [postroll, setPostroll] = useState(5);
  const [cooldown, setCooldown] = useState(5);
  const [minDur, setMinDur] = useState(0);
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
      setSensitivity(nearestSens(camera.motionThreshold || 0.02));
      setPreroll(Math.round((camera.prerollMs || 5000) / 1000));
      setPostroll(Math.round((camera.postrollMs || 5000) / 1000));
      setCooldown(Math.round((camera.cooldownMs || 5000) / 1000));
      setMinDur(Math.round((camera.minDurationMs || 0) / 1000));
    } else {
      setName("");
      setMain("");
      setSub("");
      setMode("continuous");
      setRetention(7);
      setEnabled(true);
      setSensitivity(0.02);
      setPreroll(5);
      setPostroll(5);
      setCooldown(5);
      setMinDur(0);
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
      motionThreshold: sensitivity,
      prerollMs: preroll * 1000,
      postrollMs: postroll * 1000,
      cooldownMs: cooldown * 1000,
      minDurationMs: minDur * 1000,
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

          <div className="border-t border-line pt-4">
            <p className="text-xs font-medium text-fg-muted mb-3">Motion detection</p>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="label">Sensitivity</label>
                <select
                  className="input"
                  value={sensitivity}
                  onChange={(e) => setSensitivity(Number(e.target.value))}
                >
                  {SENS.map((s) => (
                    <option key={s.v} value={s.v}>
                      {s.label}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="label">Ignore blips under (s)</label>
                <input
                  type="number"
                  min={0}
                  className="input"
                  value={minDur}
                  onChange={(e) => setMinDur(Math.max(0, Number(e.target.value) || 0))}
                />
              </div>
              <div>
                <label className="label">Pre-roll (s)</label>
                <input
                  type="number"
                  min={0}
                  className="input"
                  value={preroll}
                  onChange={(e) => setPreroll(Math.max(0, Number(e.target.value) || 0))}
                />
              </div>
              <div>
                <label className="label">Post-roll (s)</label>
                <input
                  type="number"
                  min={0}
                  className="input"
                  value={postroll}
                  onChange={(e) => setPostroll(Math.max(0, Number(e.target.value) || 0))}
                />
              </div>
              <div>
                <label className="label">Cooldown (s)</label>
                <input
                  type="number"
                  min={1}
                  className="input"
                  value={cooldown}
                  onChange={(e) => setCooldown(Math.max(1, Number(e.target.value) || 1))}
                />
              </div>
            </div>
            <p className="text-xs text-fg-faint mt-2 leading-relaxed">
              Higher sensitivity catches more motion (and more false alarms). Pre/post-roll pad each
              clip; cooldown coalesces a flurry into one event; “ignore blips” drops events shorter
              than the set seconds (0 = off).
            </p>
          </div>

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
