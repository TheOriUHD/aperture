import { useEffect, useState, type ReactNode } from "react";
import { Trash2, CalendarClock, Brush, Check, PlugZap, AlertTriangle } from "lucide-react";
import { useCapacity } from "../hooks/useHardware";
import { pctText } from "./CapacityMeter";
import { limitText } from "../lib/capacity";
import { DetailSection } from "./ui/Layout";
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
import { hasLoginMask, isProtectLink, keyMasked, maskedLoginProblem } from "../lib/cameraLink";

const SENS: { v: number; label: string }[] = [
  { v: 0.06, label: "Low" },
  { v: 0.03, label: "Medium-low" },
  { v: 0.02, label: "Medium" },
  { v: 0.012, label: "High" },
  { v: 0.006, label: "Very high" },
];
// Recording quality ceilings; 0 keeps what the camera sends. Values above the
// camera's own never upscale or pad frames, so they are safe to offer to every camera.
const RECORD_HEIGHTS: [number, string][] = [
  [0, "Original"],
  [2160, "2160p (4K)"],
  [1440, "1440p"],
  [1080, "1080p"],
  [720, "720p"],
  [480, "480p"],
];
const RECORD_FPS: [number, string][] = [
  [0, "Original"],
  [30, "30 fps"],
  [25, "25 fps"],
  [20, "20 fps"],
  [15, "15 fps"],
  [10, "10 fps"],
  [5, "5 fps"],
];
const RECORD_KBPS: [number, string][] = [
  [0, "Automatic"],
  [8000, "8 Mbps"],
  [4000, "4 Mbps"],
  [2000, "2 Mbps"],
  [1000, "1 Mbps"],
  [500, "500 kbps"],
];

function nearestSens(v: number): number {
  return SENS.reduce((a, b) => (Math.abs(b.v - v) < Math.abs(a.v - v) ? b : a), SENS[2]).v;
}

export function CameraModal({
  open,
  camera,
  onClose,
  variant = "modal",
}: {
  open: boolean;
  camera: Camera | null; // null = add
  onClose: () => void;
  // "panel": render inline as the device side panel's Settings tab (no dialog).
  variant?: "modal" | "panel";
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
  // The links as the server showed them, login masked. A link still equal to its
  // shown value is not sent on save, so the saved login is kept untouched.
  const [shownMain, setShownMain] = useState("");
  const [shownSub, setShownSub] = useState("");
  const [mode, setMode] = useState<RecordMode>("continuous");
  const [retention, setRetention] = useState(7);
  const [recordHeight, setRecordHeight] = useState(0);
  const [recordFps, setRecordFps] = useState(0);
  const [recordKbps, setRecordKbps] = useState(0);
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
  const [haPublish, setHaPublish] = useState(true);
  const [locationId, setLocationId] = useState(""); // the PRIMARY destination
  // Mirror destinations: locationId -> per-location retention days (0 = inherit).
  const [mirrors, setMirrors] = useState<Map<string, number>>(new Map());
  const [groupIds, setGroupIds] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [confirmDel, setConfirmDel] = useState(false);
  const [scheduleOpen, setScheduleOpen] = useState(false);
  const [zonesOpen, setZonesOpen] = useState(false);
  const [confirmOver, setConfirmOver] = useState(false);

  // Soft capacity limit (Settings › Hardware): what this camera, with the settings
  // in the form, would make the machine do. A tested link gives the real
  // resolution; until then a new camera counts as 4K.
  const capacity = useCapacity();
  const [testSize, setTestSize] = useState<{ w: number; h: number }>();
  useEffect(() => {
    setTestSize(undefined);
    if (!testSnap) return;
    const img = new Image();
    img.onload = () => img.naturalWidth && setTestSize({ w: img.naturalWidth, h: img.naturalHeight });
    img.src = `data:image/jpeg;base64,${testSnap}`;
  }, [testSnap]);

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
      setShownMain(camera.rtspMainUrl);
      setShownSub(camera.rtspSubUrl);
      setMode(camera.recordMode);
      setRetention(camera.retentionDays);
      setRecordHeight(camera.recordHeight ?? 0);
      setRecordFps(camera.recordFps ?? 0);
      setRecordKbps(camera.recordKbps ?? 0);
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
      setHaPublish(camera.haPublish ?? true);
      setLocationId(camera.locationId ?? "");
    } else {
      setName("");
      setMain("");
      setSub("");
      setShownMain("");
      setShownSub("");
      setMode("continuous");
      setRetention(7);
      setRecordHeight(0);
      setRecordFps(0);
      setRecordKbps(0);
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
      setHaPublish(true);
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

  const size = testSize ?? (camera ? capacity.sizeOf(camera.id) : undefined);
  const counted = isEdit ? enabled : true;
  const projected =
    capacity.configured && counted && (open || variant === "panel")
      ? capacity.project(
          capacity.workFor(
            {
              id: camera?.id ?? "new",
              name: name.trim() || "New camera",
              recordMode: mode,
              motionDetection: motionDet,
              rtspSubUrl: sub.trim(),
              recordHeight,
              recordFps,
              recordKbps,
              detectMethod,
              locationId,
            },
            size,
          ),
          camera?.id,
        )
      : undefined;
  const nowPct = capacity.capacity?.pct ?? 0;
  // Warn when these settings go over 100%; when editing, only if they add load.
  const overCapacity = !!projected && projected.pct > 1 && (!isEdit || projected.pct > nowPct + 0.005);

  // Links carrying the masked login may only keep it for the address it was saved
  // for; say so under the field instead of letting the save bounce.
  const saved = [shownMain, shownSub];
  const mainProblem = maskedLoginProblem(main.trim(), saved);
  const subProblem = maskedLoginProblem(sub.trim(), saved);

  // A UniFi Protect camera starts as live view only: Protect does the recording and
  // the motion detection, and Home Assistant usually has it through its own Protect
  // integration. Applied once, when a new camera's link first becomes one.
  function onMainChange(value: string) {
    if (!isEdit && isProtectLink(value) && !isProtectLink(main)) {
      setMode("never");
      setMotionDet(false);
      setHaPublish(false);
    }
    setMain(value);
  }

  async function save() {
    if (!name.trim() || !main.trim()) {
      setError("Name and main RTSP URL are required.");
      return;
    }
    if (mainProblem || subProblem) {
      setError(mainProblem || subProblem);
      return;
    }
    const mainOut = main.trim();
    const subOut = sub.trim();
    const input = {
      name: name.trim(),
      // Unchanged links stay out of an edit: the server keeps what is saved.
      ...(isEdit && mainOut === shownMain ? {} : { rtspMainUrl: mainOut }),
      ...(isEdit && subOut === shownSub ? {} : { rtspSubUrl: subOut }),
      recordMode: mode,
      retentionDays: retention,
      recordHeight,
      recordFps,
      recordKbps,
      enabled,
      motionThreshold: sensitivity,
      motionDetection: motionDet,
      audioDetection: audioDet,
      audioThreshold,
      detectMethod,
      tempSensor,
      vendor: isReolink ? "reolink" : "",
      haPublish,
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
        const created = await create.mutateAsync({ ...input, rtspMainUrl: mainOut });
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
    } catch (e) {
      // The server explains a refused link (e.g. a saved login on a new address).
      setError(e instanceof Error && e.message ? e.message : "Could not save the camera.");
    }
  }

  async function handleTest() {
    if (!main.trim()) {
      setError("Enter the main RTSP URL first.");
      return;
    }
    setTestSnap(null);
    setTestErr("");
    if (mainProblem) {
      setTestErr(mainProblem);
      return;
    }
    try {
      // A masked link is tested with the camera's saved login filled back in.
      const r = await test.mutateAsync({ rtspMainUrl: main.trim(), cameraId: camera?.id });
      if (r.ok && r.snapshot) setTestSnap(r.snapshot);
      else setTestErr(r.error || "No video from the camera.");
    } catch (e) {
      setTestErr(e instanceof Error && e.message ? e.message : "Test failed — could not reach the server.");
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

  const sections = (
    <>
      {overCapacity && projected && (
        <div className="flex gap-3 rounded-md border border-warn/40 bg-warn/10 p-3">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warn" />
          <div className="min-w-0 text-sm">
            <p className="font-medium text-fg">
              {isEdit
                ? `These settings take Aperture to ${pctText(projected.pct)} of its capacity`
                : nowPct > 1
                  ? `Aperture is already over its capacity (${pctText(nowPct)})`
                  : `This camera takes Aperture to ${pctText(projected.pct)} of its capacity`}
            </p>
            <p className="hint mt-0.5">
              More than this hardware runs well is not recommended for performance: recording, live view and playback may
              stutter. Limited by {limitText(projected.limitedBy)}.
              {!size && " Counted as a 4K camera until Test connection shows its real resolution."}
            </p>
          </div>
        </div>
      )}
      <EditorGroup variant={variant} title="General">
        {/* Whether the camera is on at all comes first: it governs every setting
            below it, and switching it off removes the camera from the dashboard and
            playback entirely. */}
        {isEdit && (
          <ToggleRow
            title="Enabled"
            hint="Off stops streaming and recording, and hides this camera from the dashboard and playback."
            checked={enabled}
            onChange={setEnabled}
          />
        )}
        <div>
          <label className="label">Camera name</label>
          <input className="input" placeholder="Front Door" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
      </EditorGroup>

      <EditorGroup variant={variant} title="Streams">
        <div>
          <label className="label">Main stream (RTSP URL)</label>
          <input
            className="input font-mono text-xs"
            placeholder="rtsp://user:pass@192.168.1.50:554/stream1"
            value={main}
            onChange={(e) => onMainChange(e.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
          {mainProblem && <p className="mt-1.5 text-xs text-danger">{mainProblem}</p>}
          {!isEdit && isProtectLink(main) && (
            <p className="hint mt-1.5">
              UniFi Protect link: set up for live view only, with no recording, no motion detection, and kept out of
              Home Assistant and HomeKit. Change any of that below.
            </p>
          )}
        </div>
        <div>
          <label className="label">Sub stream (optional, used for motion)</label>
          <input
            className="input font-mono text-xs"
            placeholder="rtsp://user:pass@192.168.1.50:554/stream2"
            value={sub}
            onChange={(e) => setSub(e.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
          {subProblem && <p className="mt-1.5 text-xs text-danger">{subProblem}</p>}
          {/* The saved login never reaches the browser, only a placeholder. */}
          {(hasLoginMask(main) || hasLoginMask(sub)) && !mainProblem && !subProblem && (
            <p className="hint mt-1.5">
              {keyMasked(main) || keyMasked(sub)
                ? "The stream key is hidden. Leave the dots to keep it, or paste a new link to change it."
                : "The username and password are hidden. Leave the dots to keep them, or type over them to change them."}
            </p>
          )}
        </div>
        <div>
          <button type="button" className="btn-ghost" onClick={handleTest} disabled={!main.trim() || test.isPending}>
            <PlugZap className="h-4 w-4" />
            {test.isPending ? "Testing…" : "Test connection"}
          </button>
          {testErr && <p className="mt-2 text-xs text-danger">{testErr}</p>}
          {testSnap && (
            <img
              src={`data:image/jpeg;base64,${testSnap}`}
              alt="Camera preview"
              className="mt-3 w-full rounded-md border border-line"
            />
          )}
        </div>
      </EditorGroup>

      <EditorGroup variant={variant} title="Recording">
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="label">Recording mode</label>
            <select className="input" value={mode} onChange={(e) => setMode(e.target.value as RecordMode)}>
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
            <button type="button" className="btn-ghost w-full" onClick={() => setScheduleOpen(true)}>
              <CalendarClock className="h-4 w-4" />
              Edit recording schedule
            </button>
          ) : (
            <p className="hint">Save the camera first, then open it again to set its recording schedule.</p>
          ))}
        <div>
          <p className="label">Recording quality</p>
          <div className="grid grid-cols-3 gap-2.5">
            <div>
              <label className="mb-1 block text-[11px] text-fg-faint">Resolution</label>
              <select className="input pl-2.5 pr-7 text-[13px]" value={recordHeight} onChange={(e) => setRecordHeight(Number(e.target.value))}>
                {RECORD_HEIGHTS.map(([v, label]) => (
                  <option key={v} value={v}>
                    {label}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-[11px] text-fg-faint">Framerate</label>
              <select className="input pl-2.5 pr-7 text-[13px]" value={recordFps} onChange={(e) => setRecordFps(Number(e.target.value))}>
                {RECORD_FPS.map(([v, label]) => (
                  <option key={v} value={v}>
                    {label}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-[11px] text-fg-faint">Bitrate</label>
              <select className="input pl-2.5 pr-7 text-[13px]" value={recordKbps} onChange={(e) => setRecordKbps(Number(e.target.value))}>
                {RECORD_KBPS.map(([v, label]) => (
                  <option key={v} value={v}>
                    {label}
                  </option>
                ))}
              </select>
            </div>
          </div>
          <p className="hint mt-1.5">
            {recordHeight || recordFps || recordKbps
              ? "Recordings are re-encoded to H.264 to save space, which takes about one CPU core per camera. Live view stays at full quality."
              : "Recorded exactly as the camera sends it, using almost no CPU. Lower any of these to save space; live view stays at full quality."}
          </p>
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
            <p className="hint mt-1.5">Where new footage is written. Existing recordings stay where they are.</p>
            {locations.length > 1 && (
              <div className="mt-3 space-y-1.5 rounded-md border border-line p-3">
                <p className="text-xs text-fg-muted">
                  Also record to — the same footage kept in more places, each with its own retention:
                </p>
                {locations
                  .filter((l) => l.id !== locationId)
                  .map((l) => {
                    const on = mirrors.has(l.id);
                    const days = mirrors.get(l.id) ?? 0;
                    return (
                      <div key={l.id || "default"} className="flex min-h-8 items-center gap-2 text-sm">
                        <label className="flex flex-1 cursor-pointer items-center gap-2.5 text-fg/90">
                          <input
                            type="checkbox"
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
                          <span className="truncate">
                            {l.name}
                            {l.isDefault ? " (default)" : ""}
                          </span>
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
                            <span className="text-[11px] text-fg-faint">days (blank = {retention})</span>
                          </>
                        )}
                      </div>
                    );
                  })}
              </div>
            )}
          </div>
        )}
      </EditorGroup>

      <EditorGroup variant={variant} title="Detection">
        <div>
          <ToggleRow
            title="Motion detection"
            hint={
              mode === "motion"
                ? "Always on — motion-only recording needs the trigger."
                : "Off = record per the mode above but raise no motion events for this camera."
            }
            checked={mode === "motion" || motionDet}
            onChange={mode === "motion" ? () => undefined : setMotionDet}
          />
          {mode === "motion" || motionDet ? (
            <div className="mt-4 space-y-4">
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="label">Sensitivity</label>
                  <select className="input" value={sensitivity} onChange={(e) => setSensitivity(Number(e.target.value))}>
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
              <p className="hint">
                Higher sensitivity catches more motion (and more false alarms). Pre/post-roll pad each clip; cooldown
                coalesces a flurry into one event; “ignore blips” drops events shorter than the set seconds (0 = off).
              </p>
              {isEdit ? (
                <button type="button" className="btn-ghost w-full" onClick={() => setZonesOpen(true)}>
                  <Brush className="h-4 w-4" />
                  Edit motion zones
                </button>
              ) : (
                <p className="hint">Save the camera first to paint motion zones.</p>
              )}

              {/* Object detection only runs on motion frames, so it lives under motion. */}
              <div>
                <label className="label">Object recognition</label>
                <select
                  className="input"
                  value={detectMethod === "onnx" ? "local" : detectMethod}
                  onChange={(e) => setDetectMethod(e.target.value)}
                >
                  <option value="">Same as Settings</option>
                  <option value="local">On (this server)</option>
                  <option value="off">Off for this camera</option>
                  <option value="bridge">External detector</option>
                </select>
                <p className="hint mt-1.5">
                  Recognises people, vehicles and animals while this camera sees motion — e.g. on for the driveway, off
                  indoors. Set it up in Settings → Detection.
                </p>
              </div>
            </div>
          ) : null}
        </div>

        {/* Audio detection — independent of motion; needs a mic-equipped camera. */}
        <div className="border-t border-line pt-4">
          <ToggleRow
            title="Audio detection"
            hint="Raises an “audio” event when this camera’s sound gets loud (needs a camera with a microphone)."
            checked={audioDet}
            onChange={setAudioDet}
          />
          {audioDet && (
            <div className="mt-3">
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
                className="mt-1.5 w-full"
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
      </EditorGroup>

      {isEdit && camera && (
        <EditorGroup variant={variant} title="Picture & controls" defaultOpen={variant !== "panel"}>
          <CameraOnvifPanel cameraId={camera.id} vendorMarked={isReolink} />
          {/* Vendor-specific local controls. This is a STATED FACT about the camera,
              not something detected: identifying the vendor by probing it means the
              controls disappear whenever the camera is unreachable or its ONVIF
              settings change, which is how they were previously lost. */}
          <div className="border-t border-line pt-4">
            <ToggleRow
              title="Reolink camera"
              hint={
                <>
                  Enables Reolink&apos;s own controls for this camera — spotlight and IR illuminators — which ONVIF does
                  not cover. They sign in with the <span className="text-fg-muted">Camera account</span> above (the
                  stream URL&apos;s account unless you set another), and stay available whether or not the camera is
                  reachable right now.
                </>
              }
              checked={isReolink}
              onChange={setIsReolink}
            />
          </div>
        </EditorGroup>
      )}

      <EditorGroup variant={variant} title="Integrations">
        {/* On-screen conditions: swap outdoor weather for a Home Assistant sensor */}
        {(ha?.enabled || tempSensor) && (
          <div>
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
            <p className="hint mt-1.5">
              Show a Home Assistant sensor on this camera instead of outdoor weather. Add sensors via Integrations →
              Home Assistant (MQTT Statestream).
            </p>
          </div>
        )}
        <ToggleRow
          title="Show in Home Assistant & HomeKit"
          hint="Publishes this camera to Home Assistant and HomeKit. Turn it off for a camera they already have another way, such as through Home Assistant's UniFi Protect integration. HomeKit picks up a change when Aperture next restarts."
          checked={haPublish}
          onChange={setHaPublish}
        />
        {!isEdit && (
          <ToggleRow
            title="Reolink camera"
            hint="Enables Reolink's own spotlight and IR controls for this camera, which ONVIF does not cover."
            checked={isReolink}
            onChange={setIsReolink}
          />
        )}
        {/* Camera groups — which dashboard groups this camera is in. */}
        <div>
          <label className="label">Groups</label>
          {(groups?.length ?? 0) === 0 ? (
            <p className="hint">No groups yet — create them in Settings → Camera Groups.</p>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {groups!.map((g) => {
                const on = groupIds.includes(g.id);
                return (
                  <button
                    key={g.id}
                    type="button"
                    onClick={() => setGroupIds((s) => (on ? s.filter((x) => x !== g.id) : [...s, g.id]))}
                    className={`flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-xs transition-colors ${
                      on ? "border-brand/60 bg-brand/10 text-brand" : "border-line-strong text-fg-muted hover:text-fg"
                    }`}
                  >
                    {on && <Check className="h-3 w-3" />}
                    {g.name}
                  </button>
                );
              })}
            </div>
          )}
          <p className="hint mt-1.5">Which groups this camera appears in on the dashboard&apos;s view picker.</p>
        </div>
      </EditorGroup>

      {variant === "panel" && isEdit && (
        <DetailSection title="Manage">
          <button
            type="button"
            className="flex w-full items-center justify-between py-1 text-sm text-danger hover:text-[#ff7b80]"
            onClick={() => setConfirmDel(true)}
          >
            Remove camera
            <Trash2 className="h-4 w-4" />
          </button>
          <p className="hint mt-1">Deletes the camera and all of its recordings.</p>
        </DetailSection>
      )}

      {error && <p className="text-sm text-danger">{error}</p>}
    </>
  );

  const dialogs = (
    <>
      <ConfirmDialog
        open={confirmOver}
        title="Add camera anyway?"
        message={`This takes Aperture to ${projected ? pctText(projected.pct) : "over 100%"} of what its hardware runs well. It will still work, but recording, live view and playback may stutter.`}
        confirmLabel="Add anyway"
        onConfirm={() => {
          setConfirmOver(false);
          void save();
        }}
        onCancel={() => setConfirmOver(false)}
      />
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
        <ScheduleModal open={scheduleOpen} cameraId={camera.id} cameraName={camera.name} onClose={() => setScheduleOpen(false)} />
      )}

      {camera && zonesOpen && <ZoneEditor cameraId={camera.id} cameraName={camera.name} onClose={() => setZonesOpen(false)} />}
    </>
  );

  if (variant === "panel") {
    return (
      <>
        <div className="space-y-3">{sections}</div>
        {/* Save bar, pinned to the bottom of the panel while scrolling. */}
        <div className="sticky -bottom-4 z-10 -mx-4 mt-3 flex items-center justify-end gap-2 border-t border-line bg-ink-900 px-4 py-3">
          <button className="btn-ghost" onClick={onClose} disabled={busy}>
            Revert
          </button>
          <button className="btn-primary" onClick={save} disabled={busy}>
            {busy ? "Saving…" : "Save changes"}
          </button>
        </div>
        {dialogs}
      </>
    );
  }

  return (
    <>
      <Modal
        open={open}
        onClose={onClose}
        size="lg"
        title={isEdit ? "Edit camera" : "Add camera"}
        footer={
          <div className="flex w-full items-center justify-between">
            <div>
              {isEdit && (
                <button className="btn-ghost !border-rec/30 !text-danger hover:!bg-rec/10" onClick={() => setConfirmDel(true)}>
                  <Trash2 className="h-4 w-4" />
                  Delete
                </button>
              )}
            </div>
            <div className="flex gap-2">
              <button className="btn-ghost" onClick={onClose}>
                Cancel
              </button>
              <button
                className="btn-primary"
                onClick={() => (!isEdit && overCapacity ? setConfirmOver(true) : save())}
                disabled={busy}
              >
                {busy ? "Saving…" : isEdit ? "Save changes" : "Add camera"}
              </button>
            </div>
          </div>
        }
      >
        <div className="space-y-5">{sections}</div>
      </Modal>
      {dialogs}
    </>
  );
}

// ToggleRow: a setting that is a switch, with its explanation under the title.
function ToggleRow({
  title,
  hint,
  checked,
  onChange,
}: {
  title: string;
  hint?: ReactNode;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="min-w-0">
        <p className="text-sm text-fg">{title}</p>
        {hint && <p className="hint mt-0.5">{hint}</p>}
      </div>
      <div className="pt-0.5">
        <Switch checked={checked} onChange={onChange} label={title} />
      </div>
    </div>
  );
}

// The editor's sections. In the Add/Edit modal they are flat groups under small
// headings; in the device side panel they are the filled, collapsible blocks of
// the Settings tab. The fields and their behaviour are identical in both.
function EditorGroup({
  variant,
  title,
  children,
  defaultOpen = true,
}: {
  variant: "modal" | "panel";
  title: string;
  children: ReactNode;
  defaultOpen?: boolean;
}) {
  if (variant === "panel") {
    return (
      <DetailSection title={title} defaultOpen={defaultOpen} bodyClass="space-y-4">
        {children}
      </DetailSection>
    );
  }
  return (
    <section className="space-y-4 border-t border-line pt-5 first:border-t-0 first:pt-0">
      <h3 className="text-[13px] font-semibold text-fg">{title}</h3>
      {children}
    </section>
  );
}
