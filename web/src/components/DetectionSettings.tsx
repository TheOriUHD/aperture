import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowDown,
  Check,
  ChevronDown,
  Cpu,
  Download,
  Loader2,
  MonitorCog,
  Package,
  PackageOpen,
  Power,
  ScanEye,
  Trash2,
} from "lucide-react";
import { Segmented, SettingsRow } from "./ui/Layout";
import { api } from "../lib/api";
import { useCameras } from "../hooks/useCameras";
import type { DetectedObject } from "../lib/types";
import { labelName } from "../lib/liveState";
import { GPUGuide } from "./GPUGuide";

// ---- API shapes -------------------------------------------------------------

type Device = {
  id: string;
  kind: "cpu" | "gpu";
  name: string;
  usable: boolean;
  reason?: string;
  driver?: string;
  threads?: number;
  preferred?: boolean;
};
type ModelView = {
  id: string;
  name: string;
  detail: string;
  input: number;
  size: number;
  installed: boolean;
  recommended: boolean;
  ms: number;
};
type AIStatus = {
  state: "off" | "needs-download" | "installing" | "starting" | "running" | "error";
  devices: Device[];
  device: Device;
  model: ModelView;
  models: ModelView[];
  settings: { device: string; model: string };
  download: number;
  diskFree: number;
  install: {
    active: boolean;
    stage?: string;
    phase?: "download" | "unpack";
    done: number;
    total: number;
    rate?: number; // bytes a second: received, or written while unpacking
    part?: number; // unpacking: archive bytes read
    partTotal?: number;
    error?: string;
  };
  runtime?: string;
  provider?: string;
  ms?: number;
  rate?: number;
  error?: string;
  note?: string;
  files: number;
  upgrade?: Device;
  upgradeDownload?: number;
  packages?: ModelView; // the package model for this device
  packagesRunning?: boolean;
};
type PackageWatch = { checkedAt?: number; parcels: number; scene: number };
type AIResponse = {
  status: AIStatus;
  mode: string;
  enabled: boolean;
  labels?: Record<string, number>;
  sensitivity?: string;
  packages?: Record<string, PackageWatch>;
};
type Plan = { device: Device; model: string; modelName: string; download: number };

interface DetectionConfig {
  mode: "off" | "bridge" | "local";
  bridgeUrl: string;
  bridgeApi: "deepstack" | "generic";
  modelPath: string;
  minConfidence: number;
  labels: Record<string, number>;
  burst: number;
  device?: string;
  model?: string;
  sensitivity?: "low" | "normal" | "high";
  packages?: string[]; // cameras watching for packages
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

// What people choose to recognise, as groups of the model's labels.
const GROUPS: { id: string; name: string; labels: string[] }[] = [
  { id: "people", name: "People", labels: ["person"] },
  { id: "vehicles", name: "Vehicles", labels: ["car", "truck", "bus", "motorcycle", "bicycle"] },
  { id: "animals", name: "Animals", labels: ["dog", "cat", "bird", "horse", "bear", "cow", "sheep"] },
];

// speed formats bytes a second.
export function speed(n: number): string {
  if (n >= 1024 ** 3) return `${(n / 1024 ** 3).toFixed(2)} GB/s`;
  if (n >= 1024 ** 2) return `${(n / 1024 ** 2).toFixed(1)} MB/s`;
  return `${Math.max(1, Math.round(n / 1024))} KB/s`;
}

export function bytes(n: number): string {
  if (n >= 1024 ** 3) return `${(n / 1024 ** 3).toFixed(1)} GB`;
  if (n >= 1024 ** 2) return `${Math.round(n / 1024 ** 2)} MB`;
  if (n >= 1024) return `${Math.round(n / 1024)} KB`;
  return `${n} B`;
}

// ---- panel ------------------------------------------------------------------

// Object recognition: Aperture recognises people, vehicles and animals itself,
// on this server. Setting it up is one button: it picks the hardware (an NVIDIA
// GPU when there is one, otherwise the CPU) and a model that keeps up, and
// downloads what that needs. Everything else is optional.
export function DetectionSettings() {
  const qc = useQueryClient();
  const { data: ai, refetch } = useQuery({
    queryKey: ["ai"],
    queryFn: () => api.get<AIResponse>("/api/system/ai"),
    refetchInterval: (q) => {
      const s = q.state.data?.status.state;
      return s === "installing" || s === "starting" ? 1000 : 5000;
    },
  });
  const { data: loaded } = useQuery({
    queryKey: ["detection"],
    queryFn: () => api.get<DetectionConfig>("/api/system/detection"),
  });
  const cfg = useMemo(() => ({ ...DEFAULTS, ...(loaded ?? {}), labels: loaded?.labels ?? {} }), [loaded]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [optionsOpen, setOptionsOpen] = useState(false);
  const [guide, setGuide] = useState<boolean | null>(null); // null: decide from the hardware

  const status = ai?.status;
  const enabled = cfg.mode === "local";

  async function call(fn: () => Promise<unknown>) {
    setBusy(true);
    setErr("");
    try {
      await fn();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Something went wrong");
    } finally {
      setBusy(false);
      await refetch();
      qc.invalidateQueries({ queryKey: ["detection"] });
    }
  }
  const enable = (device?: string, model?: string) =>
    call(() => api.post("/api/system/ai/enable", { device: device ?? cfg.device ?? "auto", model: model ?? cfg.model ?? "auto" }));
  const saveCfg = (patch: Partial<DetectionConfig>) =>
    call(() => api.put("/api/system/detection", { ...cfg, ...patch }));

  if (!status) {
    return (
      <div className="flex items-center gap-2 py-6 text-sm text-fg-muted">
        <Loader2 className="h-4 w-4 animate-spin" /> Loading…
      </div>
    );
  }

  return (
    <>
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <div className="min-w-0 max-w-2xl">
          <h3 className="text-sm font-semibold text-fg">Object recognition</h3>
          <p className="mt-1 text-xs leading-relaxed text-fg-faint">
            Recognises people, vehicles and animals while a camera sees motion, and shows them live in a labelled box.
            Events and notifications say what moved. It runs on this server; nothing leaves your network.
          </p>
        </div>
        {enabled && status.state === "running" && (
          <span className="pill-green shrink-0">
            <span className="h-1.5 w-1.5 rounded-full bg-online" /> Running
          </span>
        )}
      </div>

      <div className="mt-4">
        {cfg.mode === "bridge" ? (
          <BridgeNotice onSwitch={() => enable()} busy={busy} />
        ) : (
          <MainCard status={status} enabled={enabled} busy={busy} onEnable={() => enable()} onUseGPU={() => enable("auto", cfg.model ?? "auto")} onCancel={() => call(() => api.post("/api/system/ai/cancel"))} onDisable={() => call(() => api.post("/api/system/ai/disable"))} />
        )}
        {err && (
          <p className="mt-3 flex items-start gap-1.5 text-xs text-danger">
            <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" /> {err}
          </p>
        )}
        <GPUGuideToggle
          status={status}
          open={guide}
          onToggle={setGuide}
          onUseGPU={() => {
            setGuide(false);
            void enable("auto", cfg.model ?? "auto");
          }}
        />
      </div>

      {enabled && (
        <div className="mt-2 divide-y divide-line">
          <SettingsRow label="Cameras" hint="It looks while a camera sees motion." align="start">
            <CoveredCameras />
          </SettingsRow>
          <SettingsRow label="Recognise" hint="Leaves, shadows and furniture never count." align="start">
            <GroupChips labels={cfg.labels} onChange={(labels) => saveCfg({ labels })} disabled={busy} />
          </SettingsRow>
          <SettingsRow label="Sensitivity" hint="Higher finds more, and is wrong more often.">
            <Segmented
              value={cfg.sensitivity ?? "normal"}
              onChange={(v) => saveCfg({ sensitivity: v as DetectionConfig["sensitivity"] })}
              options={[
                { value: "low", label: "Low" },
                { value: "normal", label: "Normal" },
                { value: "high", label: "High" },
              ]}
            />
          </SettingsRow>
          <SettingsRow label="Packages" hint="Says when a parcel is left at the door, and when it's taken." align="start">
            <PackageCameras
              selected={cfg.packages ?? []}
              status={status}
              watches={ai?.packages ?? {}}
              onChange={(packages) => saveCfg({ packages })}
              disabled={busy}
            />
          </SettingsRow>
          <SettingsRow label="Try it" hint="Runs recognition on a live picture now.">
            <TestButton />
          </SettingsRow>
        </div>
      )}

      {cfg.mode !== "bridge" && (
        <div className="mt-4">
          <button
            type="button"
            className="flex items-center gap-1.5 text-xs font-medium text-fg-muted hover:text-fg"
            onClick={() => setOptionsOpen((o) => !o)}
            aria-expanded={optionsOpen}
          >
            <ChevronDown className={`h-3.5 w-3.5 transition-transform ${optionsOpen ? "" : "-rotate-90"}`} />
            Options: hardware, model and downloads
          </button>
          {optionsOpen && (
            <Options status={status} cfg={cfg} enabled={enabled} busy={busy} onApply={(d, m) => (enabled ? enable(d, m) : saveCfg({ device: d, model: m }))} onRemove={() => call(() => api.del("/api/system/ai/files"))} />
          )}
        </div>
      )}

      <AdvancedBridge cfg={cfg} onSave={(c) => saveCfg(c)} busy={busy} />
    </>
  );
}

// ---- NVIDIA setup guide -------------------------------------------------------

// GPUGuideToggle opens the setup guide by itself when an NVIDIA card is there
// but not usable yet; otherwise a quiet link offers it (for adding a card).
function GPUGuideToggle({
  status,
  open,
  onToggle,
  onUseGPU,
}: {
  status: AIStatus;
  open: boolean | null;
  onToggle: (v: boolean) => void;
  onUseGPU: () => void;
}) {
  const nvidia = status.devices.find((d) => d.id === "cuda");
  const running = status.provider === "cuda";
  if (running) return null;
  const shown = open ?? (!!nvidia && !nvidia.usable);
  if (!shown) {
    return (
      <button type="button" className="link mt-3 inline-flex items-center gap-1.5 text-xs" onClick={() => onToggle(true)}>
        <MonitorCog className="h-3.5 w-3.5" />
        {nvidia ? (nvidia.usable ? `${nvidia.name} · driver ready` : `Set up ${nvidia.name}`) : "Set up an NVIDIA GPU"}
      </button>
    );
  }
  return (
    <div className="mt-4">
      <GPUGuide
        onReady={nvidia?.usable ? onUseGPU : undefined}
        readyLabel={`Use the GPU${status.upgradeDownload ? ` & download (${bytes(status.upgradeDownload)})` : ""}`}
      />
      <button type="button" className="mt-2 text-xs text-fg-faint hover:text-fg-muted" onClick={() => onToggle(false)}>
        Hide the guide
      </button>
    </div>
  );
}

// ---- the main card ----------------------------------------------------------

function MainCard({
  status,
  enabled,
  busy,
  onEnable,
  onUseGPU,
  onCancel,
  onDisable,
}: {
  status: AIStatus;
  enabled: boolean;
  busy: boolean;
  onEnable: () => void;
  onUseGPU: () => void;
  onCancel: () => void;
  onDisable: () => void;
}) {
  const gpuNote = status.devices.find((d) => d.kind === "gpu" && !d.usable);
  const dev = status.device;
  const where = (
    <span className="flex items-center gap-1.5">
      {dev.kind === "gpu" ? <MonitorCog className="h-4 w-4 text-brand" /> : <Cpu className="h-4 w-4 text-brand" />}
      <span className="truncate text-fg">{dev.name}</span>
      <span className="text-fg-faint">{dev.kind === "gpu" ? "· GPU" : `· CPU, ${dev.threads ?? 1} threads`}</span>
    </span>
  );

  // Downloading.
  if (status.state === "installing") {
    const p = status.install;
    const pct = p.total ? Math.min(100, (p.done / p.total) * 100) : 0;
    const unpacking = p.phase === "unpack";
    const partPct = unpacking && p.partTotal ? Math.min(100, Math.round(((p.part ?? 0) / p.partTotal) * 100)) : null;
    return (
      <div className="card p-5">
        <div className="flex items-center justify-between gap-3">
          <div className="min-w-0">
            <p className="text-sm font-semibold text-fg">Setting up object recognition</p>
            <p className="mt-0.5 truncate text-xs text-fg-muted">{p.stage ?? "Downloading"}…</p>
          </div>
          <button className="btn-ghost shrink-0" onClick={onCancel} disabled={busy}>
            Cancel
          </button>
        </div>
        <div className="mt-4 h-1.5 overflow-hidden rounded-full bg-ink-650">
          <div className="h-full rounded-full bg-brand transition-[width] duration-500" style={{ width: `${pct}%` }} />
        </div>
        <div className="mt-2 flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-xs tabular-nums">
          <span className="text-fg-faint">
            {bytes(p.done)} of {bytes(p.total)} downloaded · {Math.round(pct)}%
          </span>
          <span className="flex items-center gap-1.5 text-fg-muted">
            {unpacking ? <PackageOpen className="h-3.5 w-3.5" /> : <ArrowDown className="h-3.5 w-3.5" />}
            {unpacking ? `Unpacking${partPct != null ? ` ${partPct}%` : ""}` : "Downloading"}
            <span className="text-fg">{p.rate ? speed(p.rate) : "…"}</span>
          </span>
        </div>
        {status.provider && (
          <p className="mt-3 text-xs text-fg-faint">
            Recognition keeps running on the {dev.kind === "gpu" ? "GPU" : "CPU"} until the new files are in place.
          </p>
        )}
      </div>
    );
  }

  // Running, starting or failed.
  if (enabled && status.state !== "needs-download" && status.state !== "off") {
    return (
      <div className="card p-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="min-w-0 space-y-1 text-sm">
            {where}
            <p className="text-xs text-fg-muted">
              {status.model.name} model ({status.model.detail.split(" · ")[0]})
              {status.state === "running" &&
                (status.ms ? (
                  <>
                    {" "}
                    · {Math.round(status.ms)} ms per picture
                    {status.rate ? ` · ${status.rate.toFixed(1)} pictures a second now` : " · waiting for motion"}
                  </>
                ) : (
                  " · waiting for motion"
                ))}
            </p>
          </div>
          <button className="btn-ghost shrink-0" onClick={onDisable} disabled={busy}>
            <Power className="h-4 w-4" /> Turn off
          </button>
        </div>
        {status.state === "starting" && (
          <p className="mt-3 flex items-center gap-1.5 text-xs text-fg-muted">
            <Loader2 className="h-3.5 w-3.5 animate-spin" /> Starting the detector…
          </p>
        )}
        {status.state === "error" && (
          <div className="mt-3 flex flex-wrap items-center gap-3">
            <p className="flex min-w-0 flex-1 items-start gap-1.5 text-xs text-danger">
              <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" /> {status.error}
            </p>
            <button className="btn-outline" onClick={onEnable} disabled={busy}>
              Try again
            </button>
          </div>
        )}
        {status.note && <p className="mt-3 text-xs leading-relaxed text-warn">{status.note}</p>}
        {status.upgrade && (
          <div className="mt-4 flex flex-wrap items-center gap-3 rounded-md bg-brand/[0.07] px-3.5 py-3">
            <MonitorCog className="h-4 w-4 shrink-0 text-brand" />
            <p className="min-w-0 flex-1 text-xs leading-relaxed text-fg">
              {status.upgrade.name} is available. It recognises far faster and more accurately than the CPU.
            </p>
            <button className="btn-primary shrink-0" onClick={() => onUseGPU()} disabled={busy}>
              Use the GPU{status.upgradeDownload ? ` & download (${bytes(status.upgradeDownload)})` : ""}
            </button>
          </div>
        )}
        {gpuNote && dev.kind !== "gpu" && <p className="mt-3 text-xs leading-relaxed text-fg-faint">{gpuNote.reason}</p>}
      </div>
    );
  }

  // Off, or on but missing files: the one button.
  const label = status.download > 0
    ? `Enable Object Recognition & Download Required Models (${bytes(status.download)})`
    : "Enable Object Recognition";
  return (
    <div className="card p-5">
      <div className="flex items-start gap-4">
        <span className="grid h-10 w-10 shrink-0 place-items-center rounded-lg bg-brand/12 text-brand">
          <ScanEye className="h-5 w-5" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-sm font-semibold text-fg">{enabled ? "A few files are missing" : "Recognise what moves"}</p>
          <div className="mt-1.5 space-y-1 text-xs text-fg-muted">
            <div className="flex flex-wrap items-center gap-x-1.5">Runs on {where}</div>
            <p>
              {status.model.name} model · about {Math.max(1, Math.round(status.model.ms))} ms per picture
              {dev.kind === "gpu" ? "" : " on this CPU"}
            </p>
          </div>
          {gpuNote && <p className="mt-2 text-xs leading-relaxed text-fg-faint">{gpuNote.reason}</p>}
          {status.error && <p className="mt-2 text-xs text-danger">{status.error}</p>}
          <button className="btn-primary mt-4 h-9 max-w-full whitespace-normal text-left" onClick={onEnable} disabled={busy}>
            {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4 shrink-0" />}
            {label}
          </button>
          {status.download > 0 && (
            <p className="mt-2 text-[11px] text-fg-faint">
              {dev.kind === "gpu"
                ? "Includes NVIDIA's CUDA libraries. Only the GPU driver must be installed on the machine."
                : "Downloads Microsoft ONNX Runtime and an open-source (Apache-2.0) model, checked against their published checksums."}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}

function BridgeNotice({ onSwitch, busy }: { onSwitch: () => void; busy: boolean }) {
  return (
    <div className="card flex flex-wrap items-center justify-between gap-3 p-5">
      <p className="min-w-0 flex-1 text-xs leading-relaxed text-fg-muted">
        Recognition uses an external detector (see below). Aperture can do it itself, live and with boxes in the live
        view.
      </p>
      <button className="btn-outline" onClick={onSwitch} disabled={busy}>
        Use Aperture's own recognition
      </button>
    </div>
  );
}

// ---- packages ---------------------------------------------------------------

// PackageCameras picks the cameras that watch for parcels. Choosing the first
// one fetches the small package model by itself (no second button).
function PackageCameras({
  selected,
  status,
  watches,
  onChange,
  disabled,
}: {
  selected: string[];
  status: AIStatus;
  watches: Record<string, PackageWatch>;
  onChange: (ids: string[]) => void;
  disabled: boolean;
}) {
  const { data: cameras } = useCameras();
  const list = watchedCameras(cameras ?? []);
  const on = new Set(selected.filter((id) => list.some((c) => c.id === id)));
  const toggle = (id: string) => {
    const next = new Set(on);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onChange([...next]);
  };
  const pm = status.packages;
  const loading = on.size > 0 && status.state === "running" && !status.packagesRunning;
  if (list.length === 0) return <p className="text-[13px] text-fg-muted">No camera is watched yet.</p>;
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-1.5">
        {list.map((c) => {
          const active = on.has(c.id);
          return (
            <button
              key={c.id}
              type="button"
              aria-pressed={active}
              disabled={disabled}
              onClick={() => toggle(c.id)}
              className={`inline-flex h-8 items-center gap-1.5 rounded-md border px-3 text-[13px] transition-colors ${
                active
                  ? "border-brand/60 bg-brand/10 text-brand"
                  : "border-line-strong text-fg-muted hover:border-[#3a3e43] hover:text-fg"
              }`}
            >
              {active ? <Check className="h-3.5 w-3.5" /> : <Package className="h-3.5 w-3.5" />}
              {c.name}
            </button>
          );
        })}
      </div>
      <p className="text-xs leading-relaxed text-fg-faint">
        A parcel counts once it stays put after whoever brought it has gone; each delivery and pick-up is an event with
        its picture, also on live-view cameras. Pick the cameras that see your door.
        {pm && !pm.installed && on.size === 0 && <> The first one downloads a small extra model ({bytes(pm.size)}).</>}
      </p>
      {loading ? (
        <p className="flex items-center gap-1.5 text-xs text-fg-muted">
          <Loader2 className="h-3.5 w-3.5 animate-spin" /> Loading the package model…
        </p>
      ) : (
        list
          .filter((c) => on.has(c.id) && watches[c.id])
          .map((c) => {
            const w = watches[c.id];
            return (
              <p key={c.id} className="flex items-center gap-1.5 text-xs text-fg-muted">
                <Package className="h-3.5 w-3.5 text-fg-faint" />
                <span className="text-fg">{c.name}</span>
                <span className="text-fg-faint">
                  ·{" "}
                  {w.parcels > 0
                    ? `${w.parcels} ${w.parcels === 1 ? "parcel" : "parcels"} waiting`
                    : w.checkedAt
                      ? "no parcel there"
                      : "taking a first look…"}
                  {w.checkedAt ? ` · looked ${ago(w.checkedAt)}` : ""}
                </span>
              </p>
            );
          })
      )}
    </div>
  );
}

function ago(ms: number): string {
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000));
  if (s < 5) return "just now";
  if (s < 60) return `${s} s ago`;
  return `${Math.round(s / 60)} min ago`;
}

// watchedCameras are the cameras local recognition watches (as CoveredCameras
// lists them).
function watchedCameras<T extends { enabled: boolean; detectMethod?: string; recordMode?: string; motionDetection?: boolean }>(
  cameras: T[],
): T[] {
  return cameras.filter((c) => {
    const method = c.detectMethod === "onnx" ? "local" : c.detectMethod || "";
    const motion = c.recordMode === "never" || c.motionDetection || c.recordMode === "motion";
    return c.enabled && motion && (method === "" || method === "local");
  });
}

// ---- which cameras ----------------------------------------------------------

// CoveredCameras lists the cameras recognition watches. It rides on motion
// detection: a recording camera needs it on; a live-only camera (recording
// off) is watched for the live view only, without events.
function CoveredCameras() {
  const { data: cameras } = useCameras();
  const list = (cameras ?? []).filter((c) => c.enabled);
  type Cam = (typeof list)[number];
  const method = (c: Cam) => (c.detectMethod === "onnx" ? "local" : c.detectMethod || "");
  const liveOnly = (c: Cam) => c.recordMode === "never";
  const motion = (c: Cam) => liveOnly(c) || c.motionDetection || c.recordMode === "motion";
  const on = list.filter((c) => motion(c) && (method(c) === "" || method(c) === "local"));
  const noMotion = list.filter((c) => !motion(c) && method(c) !== "off" && method(c) !== "bridge");
  const off = list.filter((c) => method(c) === "off" || method(c) === "bridge");
  return (
    <div className="space-y-1.5 text-[13px]">
      {on.length ? (
        <p className="text-fg">
          {on.map((c, i) => (
            <span key={c.id}>
              {i > 0 && " · "}
              {c.name}
              {liveOnly(c) && <span className="text-fg-faint"> (live view)</span>}
            </span>
          ))}
        </p>
      ) : (
        <p className="text-fg-muted">No camera is watched yet.</p>
      )}
      {on.some(liveOnly) && (
        <p className="text-xs leading-relaxed text-fg-faint">
          Live-view cameras record nothing, so they show boxes live but don't create events.
        </p>
      )}
      {noMotion.length > 0 && (
        <p className="text-xs leading-relaxed text-fg-faint">
          Not on {noMotion.map((c) => c.name).join(", ")}: motion detection is off for {noMotion.length === 1 ? "it" : "them"}.
        </p>
      )}
      {off.length > 0 && <p className="text-xs text-fg-faint">Turned off on {off.map((c) => c.name).join(", ")}.</p>}
    </div>
  );
}

// ---- what to recognise ------------------------------------------------------

function GroupChips({
  labels,
  onChange,
  disabled,
}: {
  labels: Record<string, number>;
  onChange: (labels: Record<string, number>) => void;
  disabled: boolean;
}) {
  // No list = the default: all three groups.
  const custom = Object.keys(labels).length > 0;
  const on = (g: (typeof GROUPS)[number]) => !custom || g.labels.some((l) => l in labels);
  function toggle(g: (typeof GROUPS)[number]) {
    const next = GROUPS.filter((x) => (x.id === g.id ? !on(x) : on(x)));
    if (next.length === 0) return; // keep at least one
    if (next.length === GROUPS.length) return onChange({});
    const out: Record<string, number> = {};
    for (const x of next) for (const l of x.labels) out[l] = labels[l] ?? 0;
    onChange(out);
  }
  return (
    <div className="flex flex-wrap gap-1.5">
      {GROUPS.map((g) => {
        const active = on(g);
        return (
          <button
            key={g.id}
            type="button"
            aria-pressed={active}
            disabled={disabled}
            onClick={() => toggle(g)}
            className={`inline-flex h-8 items-center gap-1.5 rounded-md border px-3 text-[13px] transition-colors ${
              active
                ? "border-brand/60 bg-brand/10 text-brand"
                : "border-line-strong text-fg-muted hover:border-[#3a3e43] hover:text-fg"
            }`}
          >
            {active && <Check className="h-3.5 w-3.5" />}
            {g.name}
          </button>
        );
      })}
    </div>
  );
}

// ---- options ----------------------------------------------------------------

function Options({
  status,
  cfg,
  enabled,
  busy,
  onApply,
  onRemove,
}: {
  status: AIStatus;
  cfg: DetectionConfig;
  enabled: boolean;
  busy: boolean;
  onApply: (device: string, model: string) => void;
  onRemove: () => void;
}) {
  const [device, setDevice] = useState(cfg.device || "auto");
  const [model, setModel] = useState(cfg.model || "auto");
  useEffect(() => {
    setDevice(cfg.device || "auto");
    setModel(cfg.model || "auto");
  }, [cfg.device, cfg.model]);
  const changed = device !== (cfg.device || "auto") || model !== (cfg.model || "auto");
  const { data: plan } = useQuery({
    queryKey: ["ai-plan", device, model],
    queryFn: () => api.get<Plan>(`/api/system/ai/plan?device=${encodeURIComponent(device)}&model=${encodeURIComponent(model)}`),
    enabled: changed,
  });
  const gpus = status.devices.filter((d) => d.kind === "gpu");
  const auto = status.devices.find((d) => d.preferred);

  const applyLabel = !changed
    ? "Saved"
    : plan && plan.download > 0
      ? enabled
        ? `Switch & download (${bytes(plan.download)})`
        : `Save (download ${bytes(plan.download)} when enabled)`
      : "Save";

  return (
    <div className="mt-3 divide-y divide-line rounded-lg border border-line [&>*]:px-4">
      <SettingsRow label="Runs on" hint={auto ? `Automatic picks ${auto.name}.` : undefined}>
        <Segmented
          value={device}
          onChange={setDevice}
          options={[
            { value: "auto", label: "Automatic" },
            { value: "cpu", label: "CPU" },
            ...gpus.filter((g) => g.id === "cuda").map((g) => ({ value: "cuda", label: "NVIDIA GPU", disabled: !g.usable })),
          ]}
        />
      </SettingsRow>
      <SettingsRow label="Model" hint="Bigger models see small and far-away things better, and need more power." align="start">
        <div className="grid max-w-xl gap-1.5">
          <ModelOption id="auto" active={model === "auto"} onPick={setModel} title="Automatic" detail={`Picks for the hardware (${status.models.find((m) => m.recommended)?.name ?? "Balanced"} now)`} />
          {status.models.map((m) => (
            <ModelOption
              key={m.id}
              id={m.id}
              active={model === m.id}
              onPick={setModel}
              title={m.name}
              detail={`${m.detail} · about ${Math.max(1, Math.round(m.ms))} ms per picture · ${bytes(m.size)}${m.installed ? " · downloaded" : ""}`}
            />
          ))}
        </div>
      </SettingsRow>
      <SettingsRow label="">
        <div className="flex flex-wrap items-center gap-3">
          {changed && (
            <button className="btn-primary" disabled={busy} onClick={() => onApply(device, model)}>
              {applyLabel}
            </button>
          )}
          {!enabled && status.files > 0 && (
            <button className="btn-ghost" onClick={onRemove} disabled={busy}>
              <Trash2 className="h-4 w-4" /> Remove downloads ({bytes(status.files)})
            </button>
          )}
          {enabled && status.files > 0 && (
            <span className="text-xs text-fg-faint">Downloads use {bytes(status.files)} · {bytes(Math.max(0, status.diskFree))} free</span>
          )}
        </div>
      </SettingsRow>
    </div>
  );
}

function ModelOption({
  id,
  active,
  onPick,
  title,
  detail,
}: {
  id: string;
  active: boolean;
  onPick: (id: string) => void;
  title: string;
  detail: string;
}) {
  return (
    <button
      type="button"
      onClick={() => onPick(id)}
      aria-pressed={active}
      className={`flex items-start gap-2.5 rounded-md border px-3 py-2 text-left transition-colors ${
        active ? "border-brand/60 bg-brand/[0.07]" : "border-line hover:border-line-strong"
      }`}
    >
      <span
        className={`mt-0.5 grid h-4 w-4 shrink-0 place-items-center rounded-full border ${active ? "border-brand bg-brand" : "border-fg-faint"}`}
      >
        {active && <span className="h-1.5 w-1.5 rounded-full bg-white" />}
      </span>
      <span className="min-w-0">
        <span className="block text-[13px] font-medium text-fg">{title}</span>
        <span className="block text-xs text-fg-faint">{detail}</span>
      </span>
    </button>
  );
}

// ---- try it -----------------------------------------------------------------

// TestButton runs recognition on a live picture of a camera and shows what it
// found, so you can see it working without waiting for something to move.
function TestButton() {
  const { data: cameras } = useCameras();
  const list = (cameras ?? []).filter((c) => c.enabled);
  const [camId, setCamId] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; backend?: string; error?: string; detections?: DetectedObject[] } | null>(
    null,
  );
  const cam = camId || list[0]?.id || "";

  async function run() {
    if (!cam) return;
    setBusy(true);
    setResult(null);
    try {
      setResult(await api.post<typeof result>("/api/system/detection/test", { cameraId: cam }));
    } catch (e) {
      setResult({ ok: false, error: e instanceof Error ? e.message : "failed" });
    } finally {
      setBusy(false);
    }
  }

  const all = (result?.detections ?? []) as (DetectedObject & { passes?: boolean })[];
  const found = all.filter((d) => d.passes);
  const ignored = all.filter((d) => !d.passes && d.confidence >= 0.3);
  return (
    <div className="flex flex-wrap items-center gap-2">
      <select className="input w-48" value={cam} onChange={(e) => setCamId(e.target.value)} aria-label="Camera to test">
        {list.map((c) => (
          <option key={c.id} value={c.id}>
            {c.name}
          </option>
        ))}
      </select>
      <button className="btn-ghost min-w-[64px]" onClick={run} disabled={busy || !cam}>
        {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : "Try"}
      </button>
      {result && (
        <span className="text-xs">
          {result.ok ? (
            <span className="text-fg-muted">
              {found.length ? (
                <span className="text-fg">
                  {found.map((d) => `${labelName(d.label)} ${Math.round(d.confidence * 100)}%`).join(", ")}
                </span>
              ) : (
                "Nothing to report in this picture"
              )}
              {ignored.length > 0 && (
                <span className="text-fg-faint">
                  {" "}
                  · also saw {ignored.map((d) => `${labelName(d.label).toLowerCase()} ${Math.round(d.confidence * 100)}%`).join(", ")}, not counted
                </span>
              )}
            </span>
          ) : (
            <span className="text-danger">{result.error}</span>
          )}
        </span>
      )}
    </div>
  );
}

// ---- external detector (advanced) ------------------------------------------

function AdvancedBridge({ cfg, onSave, busy }: { cfg: DetectionConfig; onSave: (c: Partial<DetectionConfig>) => void; busy: boolean }) {
  const [open, setOpen] = useState(cfg.mode === "bridge");
  const [url, setUrl] = useState(cfg.bridgeUrl);
  const [api_, setApi] = useState(cfg.bridgeApi);
  const [burst, setBurst] = useState(cfg.burst);
  useEffect(() => {
    setUrl(cfg.bridgeUrl);
    setApi(cfg.bridgeApi);
    setBurst(cfg.burst);
    if (cfg.mode === "bridge") setOpen(true);
  }, [cfg.bridgeUrl, cfg.bridgeApi, cfg.burst, cfg.mode]);
  return (
    <div className="mt-6 border-t border-line pt-4">
      <button
        type="button"
        className="flex items-center gap-1.5 text-xs font-medium text-fg-muted hover:text-fg"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        <ChevronDown className={`h-3.5 w-3.5 transition-transform ${open ? "" : "-rotate-90"}`} />
        Use an external detector instead (CodeProject.AI, DeepStack)
      </button>
      {open && (
        <div className="mt-3 divide-y divide-line rounded-lg border border-line [&>*]:px-4">
          <SettingsRow label="Detector URL" align="start" hint="A detection endpoint you already run. Snapshots of each event go there.">
            <input
              className="input max-w-lg"
              placeholder="http://192.168.1.10:32168/v1/vision/detection"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              aria-label="Detector URL"
            />
          </SettingsRow>
          <SettingsRow label="Response format">
            <select className="input max-w-[280px]" value={api_} onChange={(e) => setApi(e.target.value as DetectionConfig["bridgeApi"])} aria-label="Response format">
              <option value="deepstack">DeepStack / CodeProject.AI</option>
              <option value="generic">Generic (normalized boxes)</option>
            </select>
          </SettingsRow>
          <SettingsRow label="Pictures per event">
            <Segmented value={String(burst)} onChange={(v) => setBurst(Number(v))} options={[1, 2, 3, 4, 5].map((n) => ({ value: String(n), label: String(n) }))} />
          </SettingsRow>
          <SettingsRow label="">
            <div className="flex flex-wrap gap-2">
              <button className="btn-primary" disabled={busy || !url.trim()} onClick={() => onSave({ mode: "bridge", bridgeUrl: url.trim(), bridgeApi: api_, burst })}>
                {cfg.mode === "bridge" ? "Save" : "Use this detector"}
              </button>
              {cfg.mode === "bridge" && (
                <button className="btn-ghost" disabled={busy} onClick={() => onSave({ mode: "off" })}>
                  Stop using it
                </button>
              )}
            </div>
          </SettingsRow>
        </div>
      )}
    </div>
  );
}
