import { ARCH } from "./hardware/arch";
import type { CpuSpec } from "./hardware/cpus";
import type { AccelSpec } from "./hardware/accelerators";

// Camera capacity: how much of the machine Aperture's cameras use, from the
// hardware an admin confirmed and what each camera actually makes Aperture do.
//
// Work is measured in core-equivalents: one Intel Skylake core at 4 GHz. A CPU's
// budget is the sum of its cores, each scaled by its architecture's speed and its
// sustained clock (see cpuScore). A camera's cost is the work Aperture does for it,
// scaled by its resolution — a 4K camera carries about four times the pixels of a
// 1080p one. The figures are calibrated on measurements of this software: stream
// copy recording is nearly free, decoding video (motion detection on a camera with
// no sub stream, or re-encoding a recording) is what fills a CPU.
//
// Memory and the network link are budgets too; whichever is closest to full sets
// the percentage. It is a soft limit: over 100% still runs, with less room for
// live viewing, playback and exports.

export type Tier = "HD" | "2K" | "4K" | "4K+";
export const TIERS: Tier[] = ["HD", "2K", "4K", "4K+"];
export const TIER_LABEL: Record<Tier, string> = { HD: "HD", "2K": "2K", "4K": "4K", "4K+": "4K+" };

const PX_1080P = 1920 * 1080;
const RESERVE_CPU = 0.2; // kept free for the OS, the web app, playback, exports
const RESERVE_RAM = 0.15;
const NET_USABLE = 0.7;

// Per-camera costs (core-equivalents; w = pixels relative to 1080p).
export const COST = {
  restream: 0.02, // per w: ingest, restreaming to recorders/viewers, snapshots
  viewing: 0.015, // per w: a share of live viewing and playback
  recording: 0.012, // per w: stream-copy recording, indexing, upload
  motionSub: 0.02, // fixed: decoding a small sub stream for motion
  decodeMain: 0.1, // per w: decoding the full-resolution stream
  encode1080: 0.25, // fixed: x264 "veryfast" at ≤1080p
  aiCpu: 0.15, // fixed: object detection inference on the CPU, during motion
  aiOffload: 0.02, // fixed: feeding frames to an accelerator
};
// Memory per camera (GB), from the unique (PSS) memory of the processes involved:
// a motion decoder on a sub stream ~65 MB, a stream-copy recorder ~30 MB, the
// restreamer's buffers growing with the stream, a re-encoder 0.5–0.8 GB.
const RAM = { base: 0.5, restream: 0.01, perW: 0.012, recording: 0.03, motion: 0.065, motionMainPerW: 0.05, reencode: 0.25, reencodePerW: 0.07, ai: 0.05 };

export interface HardwareProfile {
  cpuId: string;
  cpuName: string;
  threads: number; // CPU threads available to Aperture (VM vCPUs, cgroup quota)
  ramGb: number;
  nicGbps: number;
  accelerator: { id: string; name: string } | null;
  updatedAt?: number;
}

export interface CameraWork {
  id: string;
  name: string;
  width?: number; // main stream; unknown → assumed 4K
  height?: number;
  recording: boolean;
  motion: boolean;
  hasSub: boolean;
  reencode: boolean;
  reencodeHeight?: number; // output height when re-encoding to a lower resolution
  ai: boolean; // object detection whose inference runs on this machine
  remoteStorage: boolean; // records to a network share (footage crosses the link twice)
}

export interface CameraLoad {
  id: string;
  name: string;
  tier: Tier;
  w: number;
  estimated: boolean;
  cpu: number;
  ram: number;
  net: number;
  ai: number;
  share: number; // fraction of the binding budget
  notes: string[];
}

export interface Capacity {
  pct: number; // 0..∞ of the binding budget
  limitedBy: "CPU" | "Memory" | "Network" | "AI accelerator";
  cpu: { used: number; budget: number };
  ram: { used: number; budget: number };
  net: { used: number; budget: number };
  ai?: { used: number; budget: number };
  cameras: CameraLoad[];
  tiers: Record<Tier, number>;
  supports: { label: string; count: number; by: "CPU" | "Memory" | "Network" }[];
}

// cpuScore is a CPU's throughput in core-equivalents, limited to the threads
// Aperture may use. Clocks are derated from boost to a sustained all-core speed.
// The first threads count as whole cores (a VM's vCPUs normally each get one);
// threads beyond the physical cores are SMT siblings and add a quarter core each.
export function cpuScore(cpu: CpuSpec, threadsAvailable?: number): number {
  const derate = cpu.vendor === "Intel" || cpu.vendor === "AMD" ? 0.85 : 0.95;
  let coresTotal = 0;
  let smtExtra = 0;
  let cores = 0;
  for (const c of cpu.clusters) {
    const perCore = ARCH[c.arch].ipc * ((c.ghz * derate) / 4);
    coresTotal += c.n * perCore;
    if (c.smt) smtExtra += c.n * perCore * 0.25;
    cores += c.n;
  }
  const t = threadsAvailable && threadsAvailable > 0 ? Math.min(threadsAvailable, cpu.threads) : cpu.threads;
  if (t <= cores) return coresTotal * (t / cores);
  const smtThreads = cpu.threads - cores;
  return coresTotal + (smtThreads > 0 ? smtExtra * ((t - cores) / smtThreads) : 0);
}

// limitText words what limits capacity, for sentences ("limited by the processor").
export function limitText(by: Capacity["limitedBy"] | Capacity["supports"][number]["by"]): string {
  return by === "CPU" ? "the processor" : by === "Memory" ? "memory" : by === "Network" ? "the network" : "the AI accelerator";
}

export function tierOf(width?: number, height?: number): Tier {
  if (!width || !height) return "4K";
  const mp = (width * height) / 1e6;
  if (mp <= 2.5) return "HD";
  if (mp <= 5.1) return "2K";
  if (mp <= 9.5) return "4K";
  return "4K+";
}

const weightOf = (width?: number, height?: number) => (width && height ? Math.min(16, Math.max(0.25, (width * height) / PX_1080P)) : 4);
const bitrateMbps = (w: number) => 3 * Math.pow(w, 0.8);

export function cameraCost(c: CameraWork, accel?: AccelSpec | null) {
  const w = weightOf(c.width, c.height);
  const notes: string[] = [];
  let cpu = (COST.restream + COST.viewing) * w;
  let ram = RAM.restream + RAM.perW * w;
  if (c.recording) {
    cpu += COST.recording * w;
    if (!c.reencode) ram += RAM.recording;
  }
  if (c.motion) {
    if (c.hasSub) {
      cpu += COST.motionSub;
    } else {
      cpu += COST.decodeMain * w;
      ram += RAM.motionMainPerW * w;
      notes.push("Motion detection decodes the full stream (no sub stream)");
    }
    ram += RAM.motion;
  }
  // A re-encoded recording is stored (and uploaded) at its output size.
  const out = c.reencode && c.reencodeHeight && c.height ? Math.min(1, c.reencodeHeight / c.height) : 1;
  const outW = Math.max(0.25, w * out * out);
  if (c.reencode) {
    cpu += COST.decodeMain * w + COST.encode1080 * Math.max(0.5, outW);
    ram += RAM.reencode + RAM.reencodePerW * w;
    notes.push("Recording is re-encoded");
  }
  let ai = 0;
  if (c.ai) {
    if (accel) {
      cpu += COST.aiOffload;
      ai = 1;
    } else {
      cpu += COST.aiCpu;
      notes.push("Object detection runs on the CPU");
    }
    ram += RAM.ai;
  }
  // The camera stream comes in once; recording to a network share sends the
  // recording out again: the camera stream itself, or a smaller re-encode (x264 at
  // the CRF used here runs at roughly 60% of the source bitrate at that size).
  let net = bitrateMbps(w);
  if (c.recording && c.remoteStorage) net += c.reencode ? bitrateMbps(outW) * 0.6 : bitrateMbps(w);
  return { w, cpu, ram, net, ai, notes };
}

export function computeCapacity(cpu: CpuSpec, profile: HardwareProfile, cams: CameraWork[], accel?: AccelSpec | null): Capacity {
  const cpuBudget = cpuScore(cpu, profile.threads) * (1 - RESERVE_CPU);
  // Memory counts the system's own use (OS, Aperture, the restreamer) as well.
  const ramBudget = profile.ramGb * (1 - RESERVE_RAM);
  const netBudget = profile.nicGbps * 1000 * NET_USABLE;
  const costs = cams.map((c) => ({ c, k: cameraCost(c, accel) }));
  const sum = (f: (k: ReturnType<typeof cameraCost>) => number) => costs.reduce((a, x) => a + f(x.k), 0);
  const used = { cpu: sum((k) => k.cpu), ram: RAM.base + sum((k) => k.ram), net: sum((k) => k.net), ai: sum((k) => k.ai) };

  const fractions: [Capacity["limitedBy"], number][] = [
    ["CPU", used.cpu / cpuBudget],
    ["Memory", ramBudget > 0 ? used.ram / ramBudget : Infinity],
    ["Network", used.net / netBudget],
  ];
  if (accel) fractions.push(["AI accelerator", used.ai / accel.aiCameras]);
  const [limitedBy, pct] = fractions.reduce((a, b) => (b[1] > a[1] ? b : a));

  const share = (k: ReturnType<typeof cameraCost>) =>
    limitedBy === "CPU" ? k.cpu / cpuBudget : limitedBy === "Memory" ? k.ram / ramBudget : limitedBy === "Network" ? k.net / netBudget : k.ai / (accel?.aiCameras || 1);

  const tiers: Record<Tier, number> = { HD: 0, "2K": 0, "4K": 0, "4K+": 0 };
  const cameras: CameraLoad[] = costs.map(({ c, k }) => {
    const tier = tierOf(c.width, c.height);
    tiers[tier]++;
    const estimated = !c.width || !c.height;
    return {
      id: c.id,
      name: c.name,
      tier,
      w: k.w,
      estimated,
      cpu: k.cpu,
      ram: k.ram,
      net: k.net,
      ai: k.ai,
      share: share(k),
      notes: estimated ? ["Resolution unknown, counted as 4K", ...k.notes] : k.notes,
    };
  });

  // How many plain cameras (recording, motion on a sub stream) this hardware runs.
  const plain = (label: string, width: number, height: number) => {
    const k = cameraCost({ id: "", name: "", width, height, recording: true, motion: true, hasSub: true, reencode: false, ai: false, remoteStorage: false });
    const fits: [Capacity["supports"][number]["by"], number][] = [
      ["CPU", cpuBudget / k.cpu],
      ["Memory", Math.max(0, ramBudget - RAM.base) / k.ram],
      ["Network", netBudget / k.net],
    ];
    const [by, n] = fits.reduce((a, b) => (b[1] < a[1] ? b : a));
    return { label, count: Math.max(0, Math.floor(n)), by };
  };
  const supports = [plain("1080p", 1920, 1080), plain("1440p", 2560, 1440), plain("4K", 3840, 2160)];

  return {
    pct: Number.isFinite(pct) ? pct : 9.99,
    limitedBy,
    cpu: { used: used.cpu, budget: cpuBudget },
    ram: { used: used.ram, budget: ramBudget },
    net: { used: used.net, budget: netBudget },
    ai: accel ? { used: used.ai, budget: accel.aiCameras } : undefined,
    cameras,
    tiers,
    supports,
  };
}
