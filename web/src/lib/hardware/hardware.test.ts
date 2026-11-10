import { describe, expect, it } from "vitest";
import { CPUS } from "./cpus";
import { ACCELERATORS } from "./accelerators";
import { ARCH } from "./arch";
import { cpuById, matchDetected, matchDetectedAccelerator, searchAccelerators, searchCpus } from "./search";
import { computeCapacity, cpuScore, tierOf, type CameraWork, type HardwareProfile } from "../capacity";

// The backend accepts catalogue ids matching this (internal/api/hardware_handlers.go).
const BACKEND_ID = /^[a-z0-9][a-z0-9._+-]{0,79}$/;

describe("catalogue", () => {
  it("is large and every entry is well formed", () => {
    expect(CPUS.length).toBeGreaterThan(450);
    const ids = new Set<string>();
    for (const c of CPUS) {
      expect(BACKEND_ID.test(c.id), c.id).toBe(true);
      expect(ids.has(c.id), `duplicate ${c.id}`).toBe(false);
      ids.add(c.id);
      const cores = c.clusters.reduce((a, k) => a + k.n, 0);
      expect(cores, c.name).toBeGreaterThan(0);
      expect(c.threads, c.name).toBeGreaterThanOrEqual(cores);
      expect(c.threads, c.name).toBeLessThanOrEqual(cores * 2);
      for (const k of c.clusters) {
        expect(ARCH[k.arch], `${c.name}: ${k.arch}`).toBeDefined();
        expect(k.ghz, c.name).toBeGreaterThan(0.5);
        expect(k.ghz, c.name).toBeLessThan(6.5);
      }
      expect(c.year, c.name).toBeGreaterThan(2008);
      expect(cpuScore(c), c.name).toBeGreaterThan(0);
    }
    const aids = new Set<string>();
    for (const a of ACCELERATORS) {
      expect(BACKEND_ID.test(a.id), a.id).toBe(true);
      expect(aids.has(a.id), `duplicate ${a.id}`).toBe(false);
      aids.add(a.id);
      expect(a.aiCameras, a.name).toBeGreaterThan(0);
    }
  });

  it("ranks faster CPUs higher", () => {
    const s = (id: string) => cpuScore(cpuById(id)!);
    expect(s("raspberry-pi-4-model-b")).toBeLessThan(s("raspberry-pi-5"));
    expect(s("intel-celeron-j4125")).toBeLessThan(s("intel-n100"));
    expect(s("intel-n100")).toBeLessThan(s("intel-core-i5-12400"));
    expect(s("intel-core-i5-12400")).toBeLessThan(s("amd-ryzen-9-7950x"));
    expect(s("amd-ryzen-9-7950x")).toBeLessThan(s("amd-epyc-9654"));
  });
});

describe("search", () => {
  const top = (q: string) => searchCpus(q, 5)[0]?.name;
  it("finds model numbers however they are typed", () => {
    expect(top("12400")).toBe("Intel Core i5-12400");
    expect(top("i5 12400")).toBe("Intel Core i5-12400");
    expect(top("i5-12400")).toBe("Intel Core i5-12400");
    expect(top("i512400")).toBe("Intel Core i5-12400");
    expect(top("n100")).toBe("Intel N100");
    expect(top("5800x")).toBe("AMD Ryzen 7 5800X");
    expect(top("ryzen 7 5800x")).toBe("AMD Ryzen 7 5800X");
    expect(top("pi 5")).toBe("Raspberry Pi 5");
    expect(top("rk3588")).toBe("Rockchip RK3588");
    expect(top("e5 2680 v4")).toBe("Intel Xeon E5-2680 v4");
    expect(top("m2 pro")).toBe("Apple M2 Pro (12-core)");
    expect(top("ds920+")).toBe("Intel Celeron J4125");
    expect(top("v1500b")).toBe("AMD Ryzen Embedded V1500B");
    expect(top("9955hx")).toBe("AMD Ryzen 9 9955HX");
    expect(top("ms-a2")).toBe("AMD Ryzen 9 9955HX");
  });
  it("needs every word to match", () => {
    expect(searchCpus("ryzen 12400")).toEqual([]);
    expect(searchCpus("zzzz")).toEqual([]);
  });
  it("finds accelerators", () => {
    expect(searchAccelerators("coral usb")[0]?.id).toBe("coral-usb");
    expect(searchAccelerators("3060")[0]?.id).toBe("nvidia-rtx-3060");
    expect(searchAccelerators("hailo 8l")[0]?.id).toBe("hailo-8l");
  });
});

describe("matching what the server detects", () => {
  it("maps real CPU strings and board names", () => {
    expect(matchDetected("Intel(R) Core(TM) i5-12400")?.id).toBe("intel-core-i5-12400");
    expect(matchDetected("AMD Ryzen 7 5800X 8-Core Processor")?.id).toBe("amd-ryzen-7-5800x");
    expect(matchDetected("Intel(R) N100")?.id).toBe("intel-n100");
    expect(matchDetected("AMD Ryzen 9 9955HX 16-Core Processor")?.id).toBe("amd-ryzen-9-9955hx");
    expect(matchDetected("Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz")?.id).toBe("intel-xeon-e5-2680-v4");
    expect(matchDetected("Raspberry Pi 5 Model B Rev 1.0")?.id).toBe("raspberry-pi-5");
    expect(matchDetected("Raspberry Pi 4 Model B Rev 1.4")?.id).toBe("raspberry-pi-4-model-b");
  });
  it("refuses to guess for virtual CPUs", () => {
    expect(matchDetected("QEMU Virtual CPU version 2.5+")).toBeUndefined();
    expect(matchDetected("Common KVM processor")).toBeUndefined();
    expect(matchDetected("Intel Xeon Processor (Skylake, IBRS)")).toBeUndefined();
  });
  it("maps detected accelerators", () => {
    expect(matchDetectedAccelerator("Google Coral USB Accelerator")?.id).toBe("coral-usb");
    expect(matchDetectedAccelerator("NVIDIA GeForce RTX 3060")?.id).toBe("nvidia-rtx-3060");
  });
});

describe("capacity", () => {
  const n100 = cpuById("intel-n100")!;
  const profile: HardwareProfile = { cpuId: n100.id, cpuName: n100.name, threads: 4, ramGb: 16, nicGbps: 1, accelerator: null };
  const cam = (i: number, width: number, height: number, extra: Partial<CameraWork> = {}): CameraWork => ({
    id: String(i),
    name: `Cam ${i}`,
    width,
    height,
    recording: true,
    motion: true,
    hasSub: true,
    reencode: false,
    ai: false,
    remoteStorage: false,
    ...extra,
  });

  it("weighs resolutions by their pixels", () => {
    const cap = computeCapacity(n100, profile, []);
    const [p1080, p1440, p4k] = cap.supports.map((s) => s.count);
    expect(p1080).toBeGreaterThan(p1440);
    expect(p1440).toBeGreaterThan(p4k);
    // Roughly the pixel ratio (4K is 4× 1080p); the small fixed motion cost on the
    // sub stream keeps it a little under.
    expect(p1080 / p4k).toBeGreaterThan(2.5);
    expect(p1080 / p4k).toBeLessThan(4.5);
    expect(p4k).toBeGreaterThan(3); // an N100 runs a handful of 4K cameras
  });

  it("fills up and goes over", () => {
    const few = computeCapacity(n100, profile, [cam(1, 3840, 2160), cam(2, 3840, 2160)]);
    expect(few.pct).toBeGreaterThan(0);
    expect(few.pct).toBeLessThan(1);
    const many = computeCapacity(n100, profile, Array.from({ length: 40 }, (_, i) => cam(i, 3840, 2160)));
    expect(many.pct).toBeGreaterThan(1);
    expect(many.cameras.reduce((a, c) => a + c.share, 0)).toBeCloseTo(many.pct, 6);
  });

  it("charges for decoding: no sub stream, re-encoding, CPU object detection", () => {
    const plain = computeCapacity(n100, profile, [cam(1, 3840, 2160)]).cpu.used;
    const noSub = computeCapacity(n100, profile, [cam(1, 3840, 2160, { hasSub: false })]).cpu.used;
    const reenc = computeCapacity(n100, profile, [cam(1, 3840, 2160, { reencode: true, reencodeHeight: 1080 })]).cpu.used;
    const ai = computeCapacity(n100, profile, [cam(1, 3840, 2160, { ai: true })]).cpu.used;
    expect(noSub).toBeGreaterThan(plain * 2);
    expect(reenc).toBeGreaterThan(plain * 2);
    expect(ai).toBeGreaterThan(plain);
  });

  it("moves object detection onto an accelerator", () => {
    const coral = ACCELERATORS.find((a) => a.id === "coral-usb")!;
    const cams = Array.from({ length: 4 }, (_, i) => cam(i, 1920, 1080, { ai: true }));
    const onCpu = computeCapacity(n100, profile, cams);
    const onTpu = computeCapacity(n100, { ...profile, accelerator: { id: coral.id, name: coral.name } }, cams, coral);
    expect(onTpu.cpu.used).toBeLessThan(onCpu.cpu.used);
    expect(onTpu.ai?.used).toBe(4);
  });

  it("limits by the threads Aperture may use and by memory", () => {
    const r7 = cpuById("amd-ryzen-7-5800x")!;
    const full = computeCapacity(r7, { ...profile, cpuId: r7.id, threads: 16, ramGb: 64, nicGbps: 10 }, []).supports[0].count;
    const quarter = computeCapacity(r7, { ...profile, cpuId: r7.id, threads: 4, ramGb: 64, nicGbps: 10 }, []).supports[0].count;
    expect(quarter).toBeLessThan(full * 0.6);
    // A VM's vCPUs up to the core count are whole cores: 8 of 16 threads is most of the CPU.
    const eight = cpuScore(r7, 8) / cpuScore(r7);
    expect(eight).toBeGreaterThan(0.75);
    expect(eight).toBeLessThan(0.85);
    const tinyRam = computeCapacity(r7, { ...profile, cpuId: r7.id, threads: 16, ramGb: 2, nicGbps: 10 }, [cam(1, 3840, 2160)]);
    expect(tinyRam.limitedBy).toBe("Memory");
  });

  it("names resolution tiers", () => {
    expect(tierOf(1920, 1080)).toBe("HD");
    expect(tierOf(2688, 1512)).toBe("2K");
    expect(tierOf(3840, 2160)).toBe("4K");
    expect(tierOf(7680, 2160)).toBe("4K+");
    expect(tierOf()).toBe("4K");
  });
});
