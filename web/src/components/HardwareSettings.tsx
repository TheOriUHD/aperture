import { useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import { Cpu, Info, Server, Sparkles } from "lucide-react";
import { SettingsHeading, SettingsRow, Segmented } from "./ui/Layout";
import { SearchSelect } from "./ui/SearchSelect";
import { Switch } from "./ui/Switch";
import { Skeleton } from "./ui/Skeleton";
import { CapacityBar, TierChips, TIER_STYLE, pctText } from "./CapacityMeter";
import { useCapacity, useHardware, useSetHardware, type DetectedHardware } from "../hooks/useHardware";
import { ARCH } from "../lib/hardware/arch";
import { CPUS, type CpuSpec } from "../lib/hardware/cpus";
import { ACCELERATORS, type AccelSpec } from "../lib/hardware/accelerators";
import { accelById, cpuById, matchDetected, matchDetectedAccelerator, searchAccelerators, searchCpus } from "../lib/hardware/search";
import { computeCapacity, cpuScore, limitText, type HardwareProfile } from "../lib/capacity";
import { formatBytes } from "../lib/format";

const NICS = [
  { value: "0.1", label: "100 Mb" },
  { value: "1", label: "1 GbE" },
  { value: "2.5", label: "2.5 GbE" },
  { value: "5", label: "5 GbE" },
  { value: "10", label: "10 GbE" },
  { value: "25", label: "25 GbE" },
];

const coresText = (c: CpuSpec) => {
  const cores = c.clusters.reduce((a, k) => a + k.n, 0);
  const split = c.clusters.length > 1 ? ` (${c.clusters.map((k) => `${k.n}× ${ARCH[k.arch].label}`).join(" + ")})` : ` · ${ARCH[c.clusters[0].arch].label}`;
  return `${cores} cores / ${c.threads} threads${split}`;
};
const maxGhz = (c: CpuSpec) => Math.max(...c.clusters.map((k) => k.ghz));

function CpuOption({ c }: { c: CpuSpec }) {
  return (
    <div className="min-w-0">
      <div className="truncate text-sm text-fg">{c.name}</div>
      <div className="truncate text-xs text-fg-faint">
        {c.segment} · {c.year} · {coresText(c)} · up to {maxGhz(c).toFixed(1)} GHz{c.igpu ? ` · ${c.igpu}` : ""}
      </div>
    </div>
  );
}
function AccelOption({ a }: { a: AccelSpec }) {
  return (
    <div className="min-w-0">
      <div className="truncate text-sm text-fg">{a.name}</div>
      <div className="truncate text-xs text-fg-faint">
        {a.kind}
        {a.tops ? ` · ${a.tops} TOPS` : ""} · object detection for about {a.aiCameras} cameras{a.video ? ` · ${a.video}` : ""}
      </div>
    </div>
  );
}

// The threads Aperture may really use: the machine's (or VM's) logical CPUs,
// further capped by a CPU quota on the service.
function detectedThreads(d: DetectedHardware): number {
  return d.cpuQuota > 0 ? Math.max(1, Math.min(d.logicalCpus, Math.round(d.cpuQuota))) : d.logicalCpus;
}

export function HardwareSettings() {
  const { data, isLoading } = useHardware();
  const save = useSetHardware();
  const { works } = useCapacity();

  const [cpuId, setCpuId] = useState<string>();
  const [threads, setThreads] = useState("");
  const [ram, setRam] = useState("");
  const [nic, setNic] = useState("1");
  const [accelOn, setAccelOn] = useState(false);
  const [accelId, setAccelId] = useState<string>();
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState("");

  const detected = data?.detected;
  const detectedCpu = useMemo(() => (detected ? matchDetected(detected.board || detected.cpuModel) : undefined), [detected]);
  const detectedAccel = useMemo(() => detected?.accelerators.map((a) => matchDetectedAccelerator(a.name)).find(Boolean), [detected]);

  const fromDetected = () => {
    if (!detected) return;
    if (detectedCpu) setCpuId(detectedCpu.id);
    setThreads(String(detectedThreads(detected)));
    const limit = detected.memLimitBytes > 0 ? Math.min(detected.memLimitBytes, detected.memTotalBytes) : detected.memTotalBytes;
    setRam(String(Math.round((detected.memTotalBytes || limit) / 2 ** 30 * 2) / 2 || ""));
    if (detectedAccel) {
      setAccelOn(true);
      setAccelId(detectedAccel.id);
    }
  };

  // Seed the form once: from the saved profile, or else from what was detected.
  const [seeded, setSeeded] = useState(false);
  useEffect(() => {
    if (!data || seeded) return;
    setSeeded(true);
    const p = data.profile;
    if (p) {
      setCpuId(p.cpuId);
      setThreads(String(p.threads));
      setRam(String(p.ramGb));
      setNic(String(p.nicGbps));
      setAccelOn(!!p.accelerator);
      setAccelId(p.accelerator?.id);
    } else {
      fromDetected();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data]);

  const cpu = cpuById(cpuId);
  const accel = accelOn ? accelById(accelId) : undefined;
  const t = Math.round(Number(threads));
  const r = Number(ram);
  const n = Number(nic);
  const problem = !cpu
    ? "Pick the processor."
    : !(t >= 1 && t <= 2048)
      ? "Enter how many CPU threads Aperture may use."
      : !(r >= 0.25 && r <= 16384)
        ? "Enter the memory in GB."
        : accelOn && !accel
          ? "Pick the accelerator, or switch it off."
          : "";

  const draft: HardwareProfile | null =
    cpu && !problem
      ? { cpuId: cpu.id, cpuName: cpu.name, threads: t, ramGb: r, nicGbps: n, accelerator: accel ? { id: accel.id, name: accel.name } : null }
      : null;
  const p = data?.profile;
  const dirty =
    !!draft &&
    (!p ||
      p.cpuId !== draft.cpuId ||
      p.threads !== draft.threads ||
      p.ramGb !== draft.ramGb ||
      p.nicGbps !== draft.nicGbps ||
      (p.accelerator?.id ?? "") !== (draft.accelerator?.id ?? ""));

  const preview = draft && cpu ? computeCapacity(cpu, draft, works, accel) : undefined;

  if (isLoading || !detected) {
    return (
      <div className="space-y-3">
        <Skeleton className="h-6 w-40" />
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }

  const memLimited = detected.memLimitBytes > 0 && detected.memLimitBytes < detected.memTotalBytes;
  return (
    <div>
      <SettingsHeading description="What Aperture runs on. It decides the camera capacity shown on the dashboard, and when adding a camera warns that it would be more than this machine runs well.">
        Hardware profile
      </SettingsHeading>

      {/* What the server sees of itself. */}
      <div className="mt-4 rounded-lg border border-line p-4">
        <div className="flex flex-wrap items-start gap-4">
          <span className="grid h-10 w-10 shrink-0 place-items-center rounded-md bg-ink-700">
            <Server className="h-5 w-5 text-fg-muted" />
          </span>
          <div className="min-w-[220px] flex-1 space-y-1 text-sm">
            <p className="font-semibold text-fg">Detected on this server</p>
            <p className="text-fg/85">
              {detected.board || detected.cpuModel || "Unknown processor"}
              {detected.virtual && <span className="text-fg-faint"> · {detected.virtual}</span>}
            </p>
            <p className="text-fg-muted">
              {detected.logicalCpus} CPU threads
              {detected.cpuQuota > 0 && <> · Aperture's service may use {+detected.cpuQuota.toFixed(1)} cores</>}
              {" · "}
              {formatBytes(detected.memTotalBytes)} memory
              {memLimited && <> (service limit {formatBytes(detected.memLimitBytes)})</>}
            </p>
            <p className="text-fg-muted">
              Accelerators: {detected.accelerators.length ? detected.accelerators.map((a) => a.name).join(", ") : "none found"}
            </p>
            {detected.virtual && !detectedCpu && (
              <p className="pt-1 text-xs leading-relaxed text-fg-faint">
                A virtual machine hides the real processor behind a generic one. Pick the host&apos;s CPU below, and give the
                threads the VM has.
              </p>
            )}
          </div>
          <button className="btn-ghost" onClick={fromDetected}>
            Use detected values
          </button>
        </div>
      </div>

      <div className="mt-2 divide-y divide-line">
        <SettingsRow label="Processor" hint={`${CPUS.length} processors: desktop, laptop and mini PC, NAS, server, single-board, Apple.`} align="start">
          <div className="max-w-xl space-y-2">
            <SearchSelect<CpuSpec>
              value={cpu}
              onChange={(c) => {
                setCpuId(c.id);
                if (!threads || Number(threads) > c.threads || (cpu && Number(threads) === cpu.threads)) setThreads(String(Math.min(c.threads, detectedThreads(detected))));
              }}
              search={searchCpus}
              itemKey={(c) => c.id}
              renderItem={(c) => <CpuOption c={c} />}
              renderValue={(c) => <CpuOption c={c} />}
              placeholder="Search processors…"
              total={CPUS.length}
            />
            {detectedCpu && detectedCpu.id !== cpuId && (
              <button className="btn-link text-xs" onClick={() => setCpuId(detectedCpu.id)}>
                <Cpu className="h-3.5 w-3.5" />
                Use the detected {detectedCpu.name}
              </button>
            )}
          </div>
        </SettingsRow>

        <SettingsRow
          label="CPU threads for Aperture"
          hint={
            <>
              All of the processor&apos;s threads on bare metal; in a VM or container, the vCPUs it has
              {detected.cpuQuota > 0 && " (or the service's CPU quota, if lower)"}. Detected: {detectedThreads(detected)}.
            </>
          }
        >
          <div className="flex items-center gap-2">
            <input
              type="number"
              min={1}
              max={cpu?.threads ?? 2048}
              className="input w-28"
              value={threads}
              onChange={(e) => setThreads(e.target.value)}
            />
            {cpu && <span className="text-xs text-fg-faint">of {cpu.threads}</span>}
          </div>
        </SettingsRow>

        <SettingsRow label="Memory" hint={`Memory available to Aperture's machine or VM. Detected: ${formatBytes(detected.memTotalBytes)}.`}>
          <div className="flex items-center gap-2">
            <input type="number" min={0.25} step={0.5} className="input w-28" value={ram} onChange={(e) => setRam(e.target.value)} />
            <span className="text-xs text-fg-faint">GB</span>
          </div>
        </SettingsRow>

        <SettingsRow label="Network" hint="The link the cameras and any network storage share.">
          <Segmented value={nic} onChange={setNic} options={NICS} />
        </SettingsRow>

        <SettingsRow
          label="AI accelerator"
          align="start"
          hint="For object detection, when the detector (Detection → External server) runs on this machine. Aperture's own video work stays on the CPU."
        >
          <div className="max-w-xl space-y-3">
            <Switch checked={accelOn} onChange={setAccelOn} label="AI accelerator" />
            {accelOn && (
              <SearchSelect<AccelSpec>
                value={accelById(accelId)}
                onChange={(a) => setAccelId(a.id)}
                search={searchAccelerators}
                itemKey={(a) => a.id}
                renderItem={(a) => <AccelOption a={a} />}
                renderValue={(a) => <AccelOption a={a} />}
                placeholder="Search accelerators: Coral, Hailo, RTX 3060, Arc A380…"
                total={ACCELERATORS.length}
              />
            )}
          </div>
        </SettingsRow>

        <SettingsRow label="">
          <div className="flex flex-wrap items-center gap-3">
            <button
              className="btn-primary"
              disabled={!dirty || save.isPending}
              onClick={async () => {
                if (!draft) return;
                setError("");
                try {
                  await save.mutateAsync(draft);
                  setSaved(true);
                  setTimeout(() => setSaved(false), 2500);
                } catch (e) {
                  setError(e instanceof Error ? e.message : "Could not save the hardware.");
                }
              }}
            >
              {save.isPending ? "Saving…" : "Save hardware"}
            </button>
            {saved && !dirty && <span className="text-sm text-online">Saved.</span>}
            {problem && <span className="text-sm text-fg-faint">{problem}</span>}
            {error && <span className="text-sm text-danger">{error}</span>}
          </div>
        </SettingsRow>
      </div>

      <SettingsHeading description={dirty ? "Preview with the values above; save to use them." : undefined}>Camera capacity</SettingsHeading>
      {preview && cpu ? (
        <div className="mt-3 space-y-5">
          <div className="rounded-lg border border-line p-5">
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
              <span className={clsx("text-2xl font-semibold tabular-nums", preview.pct > 1 ? "text-danger" : preview.pct > 0.85 ? "text-warn" : "text-fg")}>
                {pctText(preview.pct)}
              </span>
              <span className="text-sm text-fg-muted">
                used · limited by {limitText(preview.limitedBy)}
                {preview.pct > 1 && " · over capacity, adding cameras is not recommended"}
              </span>
            </div>
            <CapacityBar capacity={preview} className="mt-3 h-2.5" />
            <div className="mt-3">
              <TierChips capacity={preview} all />
            </div>
            <SupportsLine supports={preview.supports} />
            <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
              <Resource label="CPU" used={preview.cpu.used} budget={preview.cpu.budget} unit="cores" digits={2} hint={`${cpu.name} · score ${cpuScore(cpu, t).toFixed(2)}`} />
              <Resource label="Memory" used={preview.ram.used} budget={preview.ram.budget} unit="GB" digits={1} />
              <Resource label="Network" used={preview.net.used} budget={preview.net.budget} unit="Mbit/s" digits={0} />
              {preview.ai && <Resource label="AI accelerator" used={preview.ai.used} budget={preview.ai.budget} unit="cameras" digits={0} />}
            </div>
          </div>

          {preview.cameras.length > 0 && (
            <table className="tbl">
              <thead>
                <tr>
                  <th>Camera</th>
                  <th>Resolution</th>
                  <th className="text-right">Share</th>
                  <th className="hidden md:table-cell">Notes</th>
                </tr>
              </thead>
              <tbody>
                {[...preview.cameras]
                  .sort((a, b) => b.share - a.share)
                  .map((c) => (
                    <tr key={c.id}>
                      <td className="text-fg">{c.name}</td>
                      <td>
                        <span className={clsx("rounded px-1.5 py-0.5 text-[11px] font-semibold", TIER_STYLE[c.tier].chip)}>{c.tier}</span>
                        {c.estimated && <span className="ml-2 text-xs text-fg-faint">assumed</span>}
                      </td>
                      <td className="text-right tabular-nums">{pctText(c.share)}</td>
                      <td className="hidden text-xs text-fg-muted md:table-cell">{c.notes.join(" · ") || "—"}</td>
                    </tr>
                  ))}
              </tbody>
            </table>
          )}

          <details className="group rounded-lg border border-line px-4 py-3 text-sm">
            <summary className="flex cursor-pointer list-none items-center gap-2 text-fg-muted hover:text-fg">
              <Info className="h-4 w-4" />
              How this is calculated
            </summary>
            <div className="mt-3 space-y-2 text-xs leading-relaxed text-fg-muted">
              <p>
                The processor&apos;s speed comes from its cores, their architecture and clock, counted in &quot;cores&quot; of
                one Skylake core at 4 GHz, limited to the threads Aperture may use. A fifth is kept free for the system, the
                web app, playback and exports.
              </p>
              <p>
                Each camera costs what Aperture does for it, scaled by its pixels (4K has four times the pixels of 1080p):
                restreaming and live viewing, recording, and motion detection — cheap on a sub stream, expensive when the
                full stream has to be decoded. Re-encoding a recording decodes and encodes again, which is the most costly
                thing a camera can ask for. Object detection costs CPU unless an accelerator does it.
              </p>
              <p>
                Memory and the network link are budgets too; the fullest one sets the percentage. It is a soft limit: going
                over still works, with less headroom for viewing and exports.
              </p>
            </div>
          </details>
        </div>
      ) : (
        <div className="mt-3 flex items-center gap-3 rounded-lg border border-dashed border-line-strong p-5 text-sm text-fg-muted">
          <Sparkles className="h-5 w-5 shrink-0 text-brand" />
          Pick the processor to see how many cameras this machine runs.
        </div>
      )}
    </div>
  );
}

function Resource({ label, used, budget, unit, digits, hint }: { label: string; used: number; budget: number; unit: string; digits: number; hint?: string }) {
  const pct = budget > 0 ? used / budget : 0;
  return (
    <div className="rounded-md bg-ink-800 p-3" title={hint}>
      <div className="flex items-baseline justify-between gap-2 text-xs">
        <span className="text-fg-muted">{label}</span>
        <span className={clsx("tabular-nums", pct > 1 ? "text-danger" : pct > 0.85 ? "text-warn" : "text-fg/85")}>{pctText(pct)}</span>
      </div>
      <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-ink-700">
        <div className={clsx("h-full rounded-full", pct > 1 ? "bg-danger" : pct > 0.85 ? "bg-warn" : "bg-brand")} style={{ width: `${Math.min(100, pct * 100)}%` }} />
      </div>
      <div className="mt-1.5 text-[11px] tabular-nums text-fg-faint">
        {used.toFixed(digits)} of {budget.toFixed(digits)} {unit}
      </div>
    </div>
  );
}

// "This hardware runs about 32 × 1080p · 21 × 1440p · 10 × 4K cameras", and what
// limits those numbers when it isn't the processor.
function SupportsLine({ supports }: { supports: { label: string; count: number; by: "CPU" | "Memory" | "Network" }[] }) {
  const same = supports.every((s) => s.by === supports[0].by);
  return (
    <div className="mt-4 text-sm text-fg-muted">
      <p>
        This hardware runs about{" "}
        {supports.map((s, i) => (
          <span key={s.label}>
            {i > 0 && " · "}
            <span className="font-medium text-fg">{s.count}</span> × {s.label}
            {!same && <span className="text-fg-faint"> ({limitText(s.by)})</span>}
          </span>
        ))}{" "}
        cameras<span className="text-fg-faint">, recording with motion detection on a sub stream</span>
        {same && <span className="text-fg-faint">; {limitText(supports[0].by)} is the limit</span>}.
      </p>
      {supports.some((s) => s.by === "Memory") && (
        <p className="mt-1 text-xs text-fg-faint">Memory runs out first here: more of it (or a bigger VM) fits more cameras.</p>
      )}
      {supports.some((s) => s.by === "Network") && (
        <p className="mt-1 text-xs text-fg-faint">The network link fills up first here: a faster link fits more cameras.</p>
      )}
    </div>
  );
}
