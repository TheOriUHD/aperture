import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ScanEye, Loader2, CheckCircle2, AlertTriangle } from "lucide-react";
import { api } from "../lib/api";
import { useCameras } from "../hooks/useCameras";
import type { DetectedObject } from "../lib/types";

interface DetectionConfig {
  mode: "off" | "bridge" | "onnx";
  bridgeUrl: string;
  bridgeApi: "deepstack" | "generic";
  modelPath: string;
  minConfidence: number;
  labels: Record<string, number>;
  burst: number;
}

const DEFAULTS: DetectionConfig = {
  mode: "off",
  bridgeUrl: "",
  bridgeApi: "deepstack",
  modelPath: "",
  minConfidence: 0.4,
  labels: {},
  burst: 1,
};

const COMMON_LABELS = ["person", "car", "truck", "bicycle", "motorcycle", "bus", "dog", "cat", "bird"];

// Object detection is opt-in: the default install ships no detector and this
// panel leaves it "off". Enabling it points events at either an external
// inference server (DeepStack / CodeProject.AI, e.g. one you already run for
// Frigate) or a local model.
export function DetectionSettings() {
  const { data: cameras } = useCameras();
  const { data: loaded } = useQuery({
    queryKey: ["detection"],
    queryFn: () => api.get<DetectionConfig>("/api/system/detection"),
  });

  const [cfg, setCfg] = useState<DetectionConfig>(DEFAULTS);
  const [savedMsg, setSavedMsg] = useState("");
  const [saving, setSaving] = useState(false);
  const [allow, setAllow] = useState<string[]>([]);

  useEffect(() => {
    if (!loaded) return;
    setCfg({ ...DEFAULTS, ...loaded, labels: loaded.labels ?? {} });
    setAllow(Object.keys(loaded.labels ?? {}));
  }, [loaded]);

  async function save() {
    setSaving(true);
    setSavedMsg("");
    // Turn the allow-list chips into the per-label threshold map.
    const labels: Record<string, number> = {};
    for (const l of allow) labels[l] = cfg.labels[l] ?? cfg.minConfidence;
    try {
      await api.put("/api/system/detection", { ...cfg, labels });
      setSavedMsg("Saved");
      setTimeout(() => setSavedMsg(""), 1500);
    } catch (e) {
      setSavedMsg(e instanceof Error ? e.message : "Failed to save");
    } finally {
      setSaving(false);
    }
  }

  function toggleLabel(l: string) {
    setAllow((a) => (a.includes(l) ? a.filter((x) => x !== l) : [...a, l]));
  }

  return (
    <div className="panel space-y-4">
      <div className="flex items-center gap-2">
        <ScanEye className="h-4 w-4 text-fg-muted" />
        <h2 className="text-sm font-medium text-fg">Object detection</h2>
        <span className="ml-auto text-[11px] text-fg-faint">opt-in · event-gated</span>
      </div>

      <p className="text-xs text-fg-muted leading-relaxed">
        Motion stays the always-on trigger. When enabled, a detector classifies a few frames per
        event and tags it (person, car, animal…), so the Alarms feed reads like a log and rules can
        notify on <em>people, not leaves</em>. The default install runs no detector.
      </p>

      {/* Mode */}
      <div>
        <label className="label">Backend</label>
        <div className="flex gap-2">
          {(["off", "bridge", "onnx"] as const).map((m) => (
            <button
              key={m}
              onClick={() => setCfg((c) => ({ ...c, mode: m }))}
              className={`flex-1 rounded-md border px-3 py-2 text-sm capitalize transition-colors ${
                cfg.mode === m ? "border-brand bg-brand/10 text-fg" : "border-line text-fg-muted hover:text-fg"
              }`}
            >
              {m === "off" ? "Off" : m === "bridge" ? "External server" : "Local model"}
            </button>
          ))}
        </div>
      </div>

      {cfg.mode === "bridge" && (
        <div className="space-y-3">
          <div>
            <label className="label">Detector URL</label>
            <input
              className="input"
              placeholder="http://192.168.1.10:32168/v1/vision/detection"
              value={cfg.bridgeUrl}
              onChange={(e) => setCfg((c) => ({ ...c, bridgeUrl: e.target.value }))}
            />
            <p className="mt-1 text-[11px] text-fg-faint">
              DeepStack / CodeProject.AI detection endpoint. Reuse a server you run (Frigate/CPAI),
              or add a local detector sidecar container — see docs/detection.md. Fully local, no cloud.
            </p>
          </div>
          <div>
            <label className="label">Response format</label>
            <select
              className="input"
              value={cfg.bridgeApi}
              onChange={(e) => setCfg((c) => ({ ...c, bridgeApi: e.target.value as DetectionConfig["bridgeApi"] }))}
            >
              <option value="deepstack">DeepStack / CodeProject.AI</option>
              <option value="generic">Generic (normalized boxes)</option>
            </select>
          </div>
        </div>
      )}

      {cfg.mode === "onnx" && (
        <div className="space-y-2">
          <div>
            <label className="label">Model path</label>
            <input
              className="input"
              placeholder="/data/models/yolov8n.onnx"
              value={cfg.modelPath}
              onChange={(e) => setCfg((c) => ({ ...c, modelPath: e.target.value }))}
            />
          </div>
          <p className="flex items-start gap-1.5 text-[11px] text-amber">
            <AlertTriangle className="h-3.5 w-3.5 shrink-0 mt-px" />
            The local model runs in-process and needs the model file present on the server. If it
            isn't available, detection stays off (no crash) — check the logs.
          </p>
        </div>
      )}

      {cfg.mode !== "off" && (
        <>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="label">Min confidence: {cfg.minConfidence.toFixed(2)}</label>
              <input
                type="range"
                min={0.1}
                max={0.9}
                step={0.05}
                value={cfg.minConfidence}
                onChange={(e) => setCfg((c) => ({ ...c, minConfidence: Number(e.target.value) }))}
                className="w-full"
              />
            </div>
            <div>
              <label className="label">Frames per event: {cfg.burst}</label>
              <input
                type="range"
                min={1}
                max={5}
                step={1}
                value={cfg.burst}
                onChange={(e) => setCfg((c) => ({ ...c, burst: Number(e.target.value) }))}
                className="w-full"
              />
            </div>
          </div>

          <div>
            <label className="label">Only these objects (none = keep all)</label>
            <div className="flex flex-wrap gap-1.5">
              {COMMON_LABELS.map((l) => (
                <button
                  key={l}
                  onClick={() => toggleLabel(l)}
                  className={`rounded-full px-2.5 py-1 text-xs capitalize border transition-colors ${
                    allow.includes(l)
                      ? "border-brand bg-brand/15 text-fg"
                      : "border-line text-fg-muted hover:text-fg"
                  }`}
                >
                  {l}
                </button>
              ))}
            </div>
          </div>
        </>
      )}

      <div className="flex items-center gap-3 pt-1">
        <button className="btn-primary h-9" onClick={save} disabled={saving}>
          {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : "Save"}
        </button>
        {cfg.mode !== "off" && <TestButton cameras={cameras ?? []} />}
        {savedMsg && (
          <span className="flex items-center gap-1 text-xs text-fg-muted">
            <CheckCircle2 className="h-3.5 w-3.5 text-online" />
            {savedMsg}
          </span>
        )}
      </div>
    </div>
  );
}

// TestButton runs the saved backend against a live snapshot of a camera and
// shows what it detected — the fast feedback loop for tuning.
function TestButton({ cameras }: { cameras: { id: string; name: string }[] }) {
  const [camId, setCamId] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; backend?: string; error?: string; detections?: DetectedObject[] } | null>(
    null,
  );
  const cam = camId || cameras[0]?.id || "";

  async function run() {
    if (!cam) return;
    setBusy(true);
    setResult(null);
    try {
      const r = await api.post<typeof result>("/api/system/detection/test", { cameraId: cam });
      setResult(r);
    } catch (e) {
      setResult({ ok: false, error: e instanceof Error ? e.message : "failed" });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex items-center gap-2">
      <select className="input h-9 w-32" value={cam} onChange={(e) => setCamId(e.target.value)}>
        {cameras.map((c) => (
          <option key={c.id} value={c.id}>
            {c.name}
          </option>
        ))}
      </select>
      <button className="btn-ghost h-9" onClick={run} disabled={busy || !cam}>
        {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : "Test"}
      </button>
      {result && (
        <span className="text-xs">
          {result.ok ? (
            <span className="text-fg-muted">
              {result.detections?.length
                ? result.detections.map((d) => `${d.label} ${(d.confidence * 100) | 0}%`).join(", ")
                : "nothing detected"}
            </span>
          ) : (
            <span className="text-rec">{result.error}</span>
          )}
        </span>
      )}
    </div>
  );
}
