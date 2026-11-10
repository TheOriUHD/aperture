import { ARCH } from "./arch";
import { CPUS, type CpuSpec } from "./cpus";
import { ACCELERATORS, type AccelSpec } from "./accelerators";

// Search for the hardware pickers. Every query word must match the item, as a
// whole word ("5800x"), the start of a word ("124" finds "12400"), or anywhere in
// the name with spaces and dashes removed ("i512400" finds "Core i5-12400"), so
// people can type model numbers the way they remember them.

const clean = (s: string) =>
  s
    .toLowerCase()
    .replace(/\(r\)|\(tm\)|®|™/g, " ")
    .replace(/\+/g, " plus ");
const words = (s: string) => clean(s).split(/[^a-z0-9]+/).filter(Boolean);
const squash = (s: string) => clean(s).replace(/[^a-z0-9]/g, "");

interface Indexed<T> {
  item: T;
  words: string[];
  squashed: string;
  year: number;
}

function index<T>(items: T[], text: (t: T) => string[], squashText: (t: T) => string[], year: (t: T) => number): Indexed<T>[] {
  return items.map((item) => ({
    item,
    words: [...new Set(text(item).flatMap(words))],
    squashed: squashText(item).map(squash).join("|"),
    year: year(item),
  }));
}

function score<T>(entry: Indexed<T>, q: string[], qSquashed: string): number {
  let total = 0;
  for (const w of q) {
    if (entry.words.includes(w)) total += 4;
    else if (entry.words.some((x) => x.startsWith(w))) total += 3;
    else if (entry.squashed.includes(w)) total += 2;
    else return 0;
  }
  if (qSquashed.length > 2 && entry.squashed.includes(qSquashed)) total += 3;
  return total;
}

function run<T>(idx: Indexed<T>[], query: string, limit: number, name: (t: T) => string): T[] {
  const q = words(query);
  if (q.length === 0) return idx.slice().sort((a, b) => b.year - a.year).slice(0, limit).map((e) => e.item);
  const qs = squash(query);
  return idx
    .map((e) => ({ e, s: score(e, q, qs) }))
    .filter((r) => r.s > 0)
    .sort((a, b) => b.s - a.s || b.e.year - a.e.year || name(a.e.item).localeCompare(name(b.e.item)))
    .slice(0, limit)
    .map((r) => r.e.item);
}

const cpuIndex = index(
  CPUS,
  (c) => [c.name, c.vendor, c.segment, ...(c.aliases ?? []), ...c.clusters.map((k) => ARCH[k.arch].label), c.igpu ?? ""],
  (c) => [c.name, ...(c.aliases ?? [])],
  (c) => c.year,
);
const accelIndex = index(
  ACCELERATORS,
  (a) => [a.name, a.vendor, a.kind, ...(a.aliases ?? [])],
  (a) => [a.name, ...(a.aliases ?? [])],
  () => 0,
);

export function searchCpus(query: string, limit = 50): CpuSpec[] {
  return run(cpuIndex, query, limit, (c) => c.name);
}
export function searchAccelerators(query: string, limit = 50): AccelSpec[] {
  return run(accelIndex, query, limit, (a) => a.name);
}

export const cpuById = (id?: string) => CPUS.find((c) => c.id === id);
export const accelById = (id?: string) => ACCELERATORS.find((a) => a.id === id);

// matchDetected maps what the server reads from /proc/cpuinfo (or a board's
// device-tree model) to a catalogue entry, or undefined when it can't be sure:
// a VM's "QEMU Virtual CPU" or "Intel Xeon Processor (Skylake)" names no model.
export function matchDetected(model: string): CpuSpec | undefined {
  const cleaned = clean(model)
    .replace(/@\s*[\d.]+\s*ghz/g, " ")
    .replace(/\b\d+-core\b/g, " ")
    .replace(/\b(cpu|processor|with radeon graphics|radeon graphics|mobile|version)\b/g, " ");
  const q = words(cleaned);
  // A board model has trailing words the catalogue doesn't ("Model B Rev 1.0"):
  // drop words from the end until everything left matches.
  for (let n = q.length; n >= 2; n--) {
    const part = q.slice(0, n);
    if (!part.some((w) => /\d/.test(w))) return undefined; // no model number left
    const ranked = cpuIndex
      .map((e) => ({ e, s: score(e, part, part.join("")) }))
      .filter((r) => r.s >= part.length * 3)
      .sort((a, b) => b.s - a.s);
    if (ranked.length === 0) continue;
    if (ranked.length > 1 && ranked[0].s === ranked[1].s) return undefined; // ambiguous
    return ranked[0].e.item;
  }
  return undefined;
}

// Accelerators the server found by device node, mapped onto catalogue entries.
export function matchDetectedAccelerator(name: string): AccelSpec | undefined {
  const n = name.toLowerCase();
  if (n.includes("coral usb")) return accelById("coral-usb");
  if (n.includes("coral")) return accelById("coral-m2");
  if (n.includes("hailo")) return accelById("hailo-8l");
  if (n.includes("rockchip npu")) return accelById("rockchip-npu-rk3588");
  if (n.includes("intel npu")) return accelById("intel-npu-mtl");
  if (n.includes("neural compute stick")) return accelById("intel-ncs2");
  if (n.startsWith("nvidia")) return searchAccelerators(name.replace(/^nvidia\s+/i, ""), 1)[0];
  return undefined;
}
