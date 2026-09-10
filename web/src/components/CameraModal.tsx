import { useEffect, useState } from "react";
import { Trash2, CalendarClock, Brush } from "lucide-react";
import { Modal } from "./ui/Modal";
import { CameraOnvifPanel } from "./CameraOnvifPanel";
import { ConfirmDialog } from "./ui/ConfirmDialog";
import { Switch } from "./ui/Switch";
import { ScheduleModal } from "./ScheduleModal";
import { ZoneEditor } from "./ZoneEditor";
import { useCreateCamera, useUpdateCamera, useDeleteCamera, useTestCamera } from "../hooks/useCameras";
import { useHA, useHASensors } from "../hooks/useSystem";
import { useCameraGroups, useSetCameraGroups } from "../hooks/useGroups";
import {
  useStorageLocations,
  useCameraDestinations,
  useSetCameraDestinations,
} from "../hooks/useStorageLocations";
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
  const { data: groups } = useCameraGroups();
  const setGroups = useSetCameraGroups();
  const { data: locData } = useStorageLocations();
  const locations = locData?.locations ?? [];
  const { data: destData } = useCameraDestinations(camera?.id);
  const setDest = useSetCameraDestinations();
  const del = useDeleteCamera();
  const test = useTestCamera();
  const { data: ha } = useHA();
  const { data: haSensors } = useHASensors(!!ha?.enabled);
  const [testSnap, setTestSnap] = useState<string | null>(null);
  const [testErr, setTestErr] = useState("");

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
  const [motionDet, setMotionDet] = useState(true);
  const [audioDet, setAudioDet] = useState(false);
  const [audioThreshold, setAudioThreshold] = useState(0);
  const [detectMethod, setDetectMethod] = useState("");
  const [tempSensor, setTempSensor] = useState("");
  const [isReolink, setIsReolink] = useState(false);
  const [locationId, setLocationId] = useState(""); // the PRIMARY destination
  // Mirror destinations: locationId -> per-location retention days (0 = inherit).
  const [mirrors, setMirrors] = useState<Map<string, number>>(new Map());
  const [groupIds, setGroupIds] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [confirmDel, setConfirmDel] = useState(false);
  const [scheduleOpen, setScheduleOpen] = useState(false);
  const [zonesOpen, setZonesOpen] = useState(false);

  useEffect(() => {
    if (!open) return;
    setError("");
    setConfirmDel(false);
    setTestSnap(null);
    setTestErr("");
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
      setMotionDet(camera.motionDetection ?? true);
      setAudioDet(camera.audioDetection ?? false);
      setAudioThreshold(camera.audioThreshold ?? 0);
      setDetectMethod(camera.detectMethod ?? "");
      setTempSensor(camera.tempSensor ?? "");
      setIsReolink((camera.vendor ?? "") === "reolink");
      setLocationId(camera.locationId ?? "");
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
      setMotionDet(true);
      setAudioDet(false);
      setAudioThreshold(0);
      setDetectMethod("");
      setTempSensor("");
      setLocationId("");
    }
  }, [open, camera]);

  // Seed the destination set (primary + mirrors) once loaded.
  useEffect(() => {
    if (!open) return;
    if (camera && destData) {
      const m = new Map<string, number>();
      for (const d of destData.destinations) if (!d.primary) m.set(d.locationId, d.retentionDays);
      setMirrors(m);
    } else if (!camera) {
      setMirrors(new Map());
    }
  }, [open, camera, destData]);

  // Seed the camera's group membership once groups + camera are known.
  useEffect(() => {
    if (!open) return;
    if (camera && groups) setGroupIds(groups.filter((g) => g.cameraIds.includes(camera.id)).map((g) => g.id));
    else if (!camera) setGroupIds([]);
  }, [open, camera, groups]);

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
      motionDetection: motionDet,
      audioDetection: audioDet,
      audioThreshold,
      detectMethod,
      tempSensor,
      vendor: isReolink ? "reolink" : "",
      locationId,
      prerollMs: preroll * 1000,
      postrollMs: postroll * 1000,
      cooldownMs: cooldown * 1000,
      minDurationMs: minDur * 1000,
    };
    try {
      let camId = camera?.id;
      if (camera) await update.mutateAsync({ id: camera.id, ...input });
      else {
        const created = await create.mutateAsync(input);
        camId = created?.id;
      }
      if (camId && groups) await setGroups.mutateAsync({ cameraId: camId, groupIds });
      if (camId) {
        await setDest.mutateAsync({
          cameraId: camId,
          primaryLocationId: locationId,
          destinations: [
            { locationId, retentionDays: 0 },
            ...[...mirrors.entries()].filter(([l]) => l !== locationId).map(([l, days]) => ({ locationId: l, retentionDays: days })),
          ],
        });
      }
      onClose();
    } catch {
      setError("Could not save the camera.");
    }
  }

  async function handleTest() {
    if (!main.trim()) {
      setError("Enter the main RTSP URL first.");
      return;
    }
    setTestSnap(null);
    setTestErr("");
    try {
      const r = await test.mutateAsync(main.trim());
      if (r.ok && r.snapshot) setTestSnap(r.snapshot);
      else setTestErr(r.error || "No video from the camera.");
    } catch {
      setTestErr("Test failed — could not reach the server.");
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
          <div>
            <button
              type="button"
              className="btn-subtle"
              onClick={handleTest}
              disabled={!main.trim() || test.isPending}
            >
              {test.isPending ? "Testing…" : "Test connection"}
            </button>
            {testErr && <p className="text-xs text-rec mt-2">{testErr}</p>}
            {testSnap && (
              <img
                src={`data:image/jpeg;base64,${testSnap}`}
                alt="Camera preview"
                className="mt-2 w-full rounded-md border border-line"
              />
            )}
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
          {/* Shown even with a single destination: hiding it made "where does this
              camera record?" invisible until a second location existed, which read
              as the setting not existing at all. */}
          {locations.length > 0 && (
            <div>
              <label className="label">Storage</label>
              <select className="input" value={locationId} onChange={(e) => setLocationId(e.target.value)}>
                {locations.map((loc) => (
                  <option key={loc.id || "default"} value={loc.id}>
                    {loc.name}
                    {loc.isDefault ? " (default)" : ""}
                  </option>
                ))}
              </select>
              <p className="text-[11px] text-fg-faint mt-1">
                Where new footage is written. Existing recordings stay where they are.
              </p>
              {locations.length > 1 && (
                <div className="mt-2 space-y-1.5">
                  <p className="text-[11px] text-fg-muted">
                    Also record to — the same footage kept in more places, each with its own retention:
                  </p>
                  {locations
                    .filter((l) => l.id !== locationId)
                    .map((l) => {
                      const on = mirrors.has(l.id);
                      const days = mirrors.get(l.id) ?? 0;
                      return (
                        <div key={l.id || "default"} className="flex items-center gap-2 text-xs">
                          <label className="flex flex-1 cursor-pointer items-center gap-2 text-fg-muted">
                            <input
                              type="checkbox"
                              className="accent-brand"
                              checked={on}
                              onChange={() =>
                                setMirrors((m) => {
                                  const n = new Map(m);
                                  if (on) n.delete(l.id);
                                  else n.set(l.id, 0);
                                  return n;
                                })
                              }
                            />
                            <span className="truncate">{l.name}{l.isDefault ? " (default)" : ""}</span>
                          </label>
                          {on && (
                            <>
                              <input
                                type="number"
                                min={0}
                                className="input h-7 w-20 text-xs"
                                placeholder={String(retention)}
                                value={days === 0 ? "" : days}
                                onChange={(e) =>
                                  setMirrors((m) => {
                                    const n = new Map(m);
                                    n.set(l.id, Math.max(0, Number(e.target.value) || 0));
                                    return n;
                                  })
                                }
                              />
                              <span className="text-[11px] text-fg-faint">days here (blank = {retention})</span>
                            </>
                          )}
                        </div>
                      );
                    })}
                </div>
              )}
            </div>
          )}
          {isEdit && camera && <CameraOnvifPanel cameraId={camera.id} />}

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
            <div className="flex items-center justify-between mb-1">
              <p className="text-xs font-medium text-fg-muted">Motion detection</p>
              <Switch
                checked={mode === "motion" || motionDet}
                onChange={mode === "motion" ? () => undefined : setMotionDet}
                label="Motion detection"
              />
            </div>
            {mode === "motion" ? (
              <p className="text-[11px] text-fg-faint mb-3">Always on — motion-only recording needs the trigger.</p>
            ) : (
              <p className="text-[11px] text-fg-faint mb-3">
                Off = record per the mode above but raise no motion events for this camera.
              </p>
            )}

            {mode === "motion" || motionDet ? (
              <>
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
                {isEdit ? (
                  <button type="button" className="btn-subtle w-full mt-3" onClick={() => setZonesOpen(true)}>
                    <Brush className="h-4 w-4" />
                    Edit motion zones
                  </button>
                ) : (
                  <p className="text-xs text-fg-faint mt-3">Save the camera first to paint motion zones.</p>
                )}

                {/* Object detection only runs on motion frames, so it lives under motion. */}
                <div className="mt-4 border-t border-line pt-3">
                  <label className="label">Object detection method</label>
                  <select className="input" value={detectMethod} onChange={(e) => setDetectMethod(e.target.value)}>
                    <option value="">Use default (from Settings)</option>
                    <option value="off">Off</option>
                    <option value="bridge">External server</option>
                    <option value="onnx">Local model</option>
                  </select>
                  <p className="text-[11px] text-fg-faint mt-1.5">
                    Overrides the global detector for this camera — e.g. run AI on the driveway, none indoors.
                    Uses the server URL / model path from Settings → Detection.
                  </p>
                </div>
              </>
            ) : null}
          </div>

          {/* Audio detection — independent of motion; needs a mic-equipped camera. */}
          <div className="border-t border-line pt-4">
            <div className="flex items-center justify-between mb-1">
              <p className="text-xs font-medium text-fg-muted">Audio detection</p>
              <Switch checked={audioDet} onChange={setAudioDet} label="Audio detection" />
            </div>
            <p className="text-[11px] text-fg-faint mb-3">
              Raises an “audio” event when this camera’s sound gets loud (needs a camera with a microphone).
            </p>
            {audioDet && (
              <div>
                <div className="flex items-baseline justify-between">
                  <label className="label mb-0">Threshold</label>
                  <span className="text-[11px] text-fg-faint">
                    {audioThreshold > 0 ? `${Math.round(audioThreshold * 100)}%` : "default"}
                  </span>
                </div>
                <input
                  type="range"
                  min={0}
                  max={0.3}
                  step={0.01}
                  className="mt-1.5 w-full accent-brand"
                  value={audioThreshold}
                  onChange={(e) => setAudioThreshold(Number(e.target.value))}
                />
                <div className="flex justify-between text-[10px] text-fg-faint">
                  <span>More sensitive</span>
                  <span>Only loud sounds</span>
                </div>
              </div>
            )}
          </div>

          {/* On-screen conditions: swap outdoor weather for a Home Assistant sensor */}
          {(ha?.enabled || tempSensor) && (
            <div className="border-t border-line pt-4">
              <label className="label">Overlay temperature</label>
              <select className="input" value={tempSensor} onChange={(e) => setTempSensor(e.target.value)}>
                <option value="">Outdoor weather (default)</option>
                {tempSensor && !haSensors?.some((s) => s.entityId === tempSensor) && (
                  <option value={tempSensor}>{tempSensor} (waiting for reading…)</option>
                )}
                {haSensors?.map((s) => (
                  <option key={s.entityId} value={s.entityId}>
                    {(s.name || s.entityId) + (s.unit ? ` — ${s.value} ${s.unit}` : ` — ${s.value}`)}
                  </option>
                ))}
              </select>
              <p className="text-[11px] text-fg-faint mt-1.5">
                Show a Home Assistant sensor on this camera instead of outdoor weather. Add sensors via
                Settings → Home Assistant (MQTT Statestream).
              </p>
            </div>
          )}

          {/* Vendor-specific local controls. This is a STATED FACT about the camera,
              not something detected: identifying the vendor by probing it means the
              controls disappear whenever the camera is unreachable or its ONVIF
              settings change, which is how they were previously lost. */}
          <div className="border-t border-line pt-4">
            <div className="mb-1 flex items-center justify-between">
              <p className="text-xs font-medium text-fg-muted">Reolink camera</p>
              <Switch checked={isReolink} onChange={setIsReolink} label="Reolink camera" />
            </div>
            <p className="text-[11px] text-fg-faint">
              Enables Reolink&apos;s own controls for this camera — spotlight and IR illuminators — which
              ONVIF does not cover. Uses the camera&apos;s existing credentials, and stays on whether or not
              the camera is reachable right now.
            </p>
          </div>

          {/* Camera groups — which mobile-dashboard groups this camera is in. */}
          <div className="border-t border-line pt-4">
            <label className="label">Groups</label>
            {(groups?.length ?? 0) === 0 ? (
              <p className="text-[11px] text-fg-faint">No groups yet — create them in Settings → Camera Groups.</p>
            ) : (
              <div className="flex flex-wrap gap-1.5">
                {groups!.map((g) => {
                  const on = groupIds.includes(g.id);
                  return (
                    <button
                      key={g.id}
                      type="button"
                      onClick={() => setGroupIds((s) => (on ? s.filter((x) => x !== g.id) : [...s, g.id]))}
                      className={`rounded-full border px-2.5 py-1 text-xs transition-colors ${
                        on ? "border-brand bg-brand/15 text-fg" : "border-line text-fg-muted hover:text-fg"
                      }`}
                    >
                      {g.name}
                    </button>
                  );
                })}
              </div>
            )}
            <p className="mt-1.5 text-[11px] text-fg-faint">
              Which groups this camera appears in on the mobile dashboard&apos;s group selector.
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

      {camera && zonesOpen && (
        <ZoneEditor cameraId={camera.id} cameraName={camera.name} onClose={() => setZonesOpen(false)} />
      )}
    </>
  );
}
