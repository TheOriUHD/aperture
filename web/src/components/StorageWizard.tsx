import { useEffect, useState } from "react";
import { HardDrive, ShieldCheck, ChevronLeft, ChevronRight, Loader2, X, Check } from "lucide-react";
import { useDiskStats, useReserveStorage, type DiskStats } from "../hooks/useStorage";
import { useScrollLock } from "../hooks/useScrollLock";
import { formatBytes } from "../lib/format";
import { donutSegments } from "../lib/donut";

const OTHER = "#3a4150";
const APERTURE = "#1b9df0";
const FREE = "#20242e";
const GiB = 1024 ** 3;

const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));

// A slice-based donut of the whole disk.
function DiskDonut({
  other,
  aperture,
  free,
  size = 208,
}: {
  other: number;
  aperture: number;
  free: number;
  size?: number;
}) {
  const thickness = 26;
  const r = (size - thickness) / 2;
  const total = other + aperture + free || 1;
  const segs = [
    { v: other, color: OTHER },
    { v: aperture, color: APERTURE },
    { v: free, color: FREE },
  ];
  const pct = Math.round((aperture / total) * 100);
  const paths = donutSegments(
    segs.map((s) => s.v),
    size / 2,
    size / 2,
    r,
  );
  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} className="shrink-0">
      <g>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="#12151c" strokeWidth={thickness} />
        {segs.map((s, i) =>
          paths[i] ? (
            <path key={i} d={paths[i]} fill="none" stroke={s.color} strokeWidth={thickness} strokeLinecap="butt" />
          ) : null,
        )}
      </g>
      <text x="50%" y="46%" textAnchor="middle" className="fill-fg" style={{ fontSize: 30, fontWeight: 700 }}>
        {pct}%
      </text>
      <text x="50%" y="58%" textAnchor="middle" className="fill-fg-faint" style={{ fontSize: 12 }}>
        to Aperture
      </text>
    </svg>
  );
}

function Swatch({ color, ring }: { color: string; ring?: boolean }) {
  return (
    <span
      className="h-2.5 w-2.5 rounded-sm shrink-0"
      style={{ backgroundColor: color, border: ring ? "1px solid #2f3542" : undefined }}
    />
  );
}

export function StorageWizard({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { data: disk } = useDiskStats(open);
  const reserve = useReserveStorage();
  const [step, setStep] = useState(1);
  const [alloc, setAlloc] = useState(0);
  const [applying, setApplying] = useState(false);
  const [seconds, setSeconds] = useState(30);
  useScrollLock(open);

  // Reset + seed the slider whenever the wizard opens or the disk data arrives.
  useEffect(() => {
    if (!open) {
      setStep(1);
      setApplying(false);
      setSeconds(30);
      return;
    }
    if (disk) {
      const seed = disk.allocatedBytes > 0 ? disk.allocatedBytes : Math.round(disk.maxAllocatableBytes * 0.6);
      setAlloc(clamp(seed, GiB, Math.max(GiB, disk.maxAllocatableBytes)));
    }
  }, [open, disk]);

  // Page-3 countdown → auto-apply.
  useEffect(() => {
    if (step !== 3 || applying) return;
    if (seconds <= 0) {
      void apply();
      return;
    }
    const t = window.setTimeout(() => setSeconds((s) => s - 1), 1000);
    return () => window.clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, seconds, applying]);

  async function apply() {
    if (applying) return;
    setApplying(true);
    try {
      await reserve.mutateAsync(alloc);
    } catch {
      /* the daemon is already exiting to restart — the request may not return */
    }
    // Wait for the service to actually go down, then poll until it's back and
    // reload into the fresh process (which applies the reservation on boot).
    const start = Date.now();
    const poll = async () => {
      try {
        const res = await fetch("/manifest.webmanifest", { cache: "no-store" });
        if (res.ok) {
          window.location.reload();
          return;
        }
      } catch {
        /* still down */
      }
      if (Date.now() - start > 30000) window.location.reload();
      else window.setTimeout(poll, 1500);
    };
    window.setTimeout(poll, 3000);
  }

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 grid place-items-center p-4" style={{ zIndex: 60 }}>
      <div className="absolute inset-0 bg-black/70 animate-fade-in" onClick={applying ? undefined : onClose} />
      <div className="relative w-[min(96vw,640px)] card overflow-hidden shadow-pop animate-fade-in">
        {/* header / progress */}
        <div className="flex items-center gap-3 px-5 pt-4 pb-3 border-b border-line">
          <HardDrive className="h-4 w-4 text-brand" />
          <span className="text-sm font-medium text-fg">Storage allocation</span>
          <div className="ml-auto flex items-center gap-1.5">
            {[1, 2, 3].map((n) => (
              <span
                key={n}
                className={`h-1.5 rounded-full transition-all ${
                  n === step ? "w-6 bg-brand" : n < step ? "w-4 bg-brand/50" : "w-4 bg-ink-700"
                }`}
              />
            ))}
            {!applying && (
              <button onClick={onClose} className="ml-2 btn-icon h-7 w-7" aria-label="Close">
                <X className="h-4 w-4" />
              </button>
            )}
          </div>
        </div>

        {applying ? (
          <Applying alloc={alloc} />
        ) : !disk ? (
          <div className="grid place-items-center py-20 text-fg-faint">
            <Loader2 className="h-5 w-5 animate-spin" />
          </div>
        ) : step === 1 ? (
          <Intro onNext={() => setStep(2)} disk={disk} />
        ) : step === 2 ? (
          <Allocate disk={disk} alloc={alloc} setAlloc={setAlloc} onBack={() => setStep(1)} onNext={() => { setSeconds(30); setStep(3); }} />
        ) : (
          <Confirm
            disk={disk}
            alloc={alloc}
            seconds={seconds}
            onBack={() => setStep(2)}
            onApplyNow={apply}
          />
        )}
      </div>
    </div>
  );
}

function Intro({ onNext, disk }: { onNext: () => void; disk: DiskStats }) {
  return (
    <div className="p-6">
      <div className="mx-auto mb-4 grid h-14 w-14 place-items-center rounded-2xl bg-brand/15">
        <ShieldCheck className="h-7 w-7 text-brand" />
      </div>
      <h3 className="text-center text-lg font-semibold text-fg">Reserve space for Aperture</h3>
      <p className="mx-auto mt-2 max-w-md text-center text-sm text-fg-muted leading-relaxed">
        You’ll set how much of this {formatBytes(disk.totalBytes)} disk belongs to Aperture. That space is{" "}
        <span className="text-fg">hard-reserved</span> — physically held for recordings so no other program can take
        it, even while Aperture is off. Aperture stays within it automatically, trimming the oldest footage as it fills.
      </p>
      <ul className="mx-auto mt-5 max-w-md space-y-2.5">
        {[
          "Pick an amount with a live view of the whole disk",
          "The rest stays free for your other data",
          "Applying restarts Aperture once (a few seconds)",
        ].map((t) => (
          <li key={t} className="flex items-start gap-2.5 text-sm text-fg-muted">
            <Check className="mt-0.5 h-4 w-4 shrink-0 text-online" />
            {t}
          </li>
        ))}
      </ul>
      <button className="btn-primary w-full mt-6" onClick={onNext}>
        Get started
        <ChevronRight className="h-4 w-4" />
      </button>
    </div>
  );
}

function Allocate({
  disk,
  alloc,
  setAlloc,
  onBack,
  onNext,
}: {
  disk: DiskStats;
  alloc: number;
  setAlloc: (v: number) => void;
  onBack: () => void;
  onNext: () => void;
}) {
  const maxAlloc = Math.max(GiB, disk.maxAllocatableBytes);
  const free = disk.totalBytes - disk.otherBytes - alloc;
  const shrinksFootage = alloc < disk.apertureUsedBytes;
  return (
    <div className="p-6">
      <div className="flex flex-col sm:flex-row items-center gap-6">
        <DiskDonut other={disk.otherBytes} aperture={alloc} free={Math.max(0, free)} />
        <div className="flex-1 w-full space-y-2.5">
          <Legend color={APERTURE} label="Aperture" value={alloc} strong />
          <Legend color={FREE} label="Free" value={Math.max(0, free)} ring />
          <Legend color={OTHER} label="Other data (locked)" value={disk.otherBytes} />
          <div className="flex justify-between border-t border-line pt-2 text-xs text-fg-muted">
            <span>Whole disk</span>
            <span className="tabular-nums">{formatBytes(disk.totalBytes)}</span>
          </div>
        </div>
      </div>

      <div className="mt-6">
        <div className="flex items-end justify-between mb-2">
          <label className="label mb-0">Allocate to Aperture</label>
          <span className="text-lg font-semibold text-fg tabular-nums">{formatBytes(alloc)}</span>
        </div>
        <input
          type="range"
          min={GiB}
          max={maxAlloc}
          step={GiB / 2}
          value={alloc}
          onChange={(e) => setAlloc(clamp(Number(e.target.value), GiB, maxAlloc))}
          className="w-full accent-brand"
        />
        <div className="flex justify-between text-[11px] text-fg-faint mt-1">
          <span>1 GB</span>
          <span>Max {formatBytes(maxAlloc)}</span>
        </div>
        {shrinksFootage ? (
          <p className="text-[11px] text-amber mt-2">
            Below your current {formatBytes(disk.apertureUsedBytes)} of footage — the oldest recordings will be deleted
            to fit.
          </p>
        ) : (
          <p className="text-[11px] text-fg-faint mt-2">
            {formatBytes(disk.marginBytes)} is always kept free so the disk can’t be filled completely.
          </p>
        )}
      </div>

      <div className="flex gap-2 mt-6">
        <button className="btn-ghost h-9" onClick={onBack}>
          <ChevronLeft className="h-4 w-4" />
          Back
        </button>
        <button className="btn-primary h-9 flex-1" onClick={onNext}>
          Continue
          <ChevronRight className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}

function Confirm({
  disk,
  alloc,
  seconds,
  onBack,
  onApplyNow,
}: {
  disk: DiskStats;
  alloc: number;
  seconds: number;
  onBack: () => void;
  onApplyNow: () => void;
}) {
  const free = Math.max(0, disk.totalBytes - disk.otherBytes - alloc);
  const size = 128;
  const r = 56;
  const c = 2 * Math.PI * r;
  const frac = seconds / 30;
  return (
    <div className="p-6">
      <div className="grid place-items-center">
        <div className="relative" style={{ width: size, height: size }}>
          <svg width={size} height={size} className="-rotate-90">
            <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="#20242e" strokeWidth={8} />
            <circle
              cx={size / 2}
              cy={size / 2}
              r={r}
              fill="none"
              stroke={APERTURE}
              strokeWidth={8}
              strokeLinecap="round"
              strokeDasharray={c}
              strokeDashoffset={c * (1 - frac)}
              style={{ transition: "stroke-dashoffset 1s linear" }}
            />
          </svg>
          <div className="absolute inset-0 grid place-items-center">
            <span className="text-3xl font-bold text-fg tabular-nums">{seconds}</span>
          </div>
        </div>
        <p className="mt-3 text-sm text-fg-muted text-center">
          Applying and restarting Aperture in <span className="text-fg font-medium">{seconds}s</span>
        </p>
      </div>

      <div className="mt-5 rounded-lg bg-ink-850 border border-line p-4 space-y-2 text-sm">
        <SummaryRow label="Hard-reserved for Aperture" value={formatBytes(alloc)} strong />
        <SummaryRow label="Free for other programs" value={formatBytes(free)} />
        <SummaryRow label="Other data (untouched)" value={formatBytes(disk.otherBytes)} />
        <div className="border-t border-line pt-2">
          <SummaryRow label="Whole disk" value={formatBytes(disk.totalBytes)} muted />
        </div>
      </div>
      <p className="text-[11px] text-fg-faint mt-2 text-center">
        The service restarts once to apply — live view and recording resume automatically.
      </p>

      <div className="flex gap-2 mt-5">
        <button className="btn-ghost h-9" onClick={onBack}>
          <ChevronLeft className="h-4 w-4" />
          Back &amp; change
        </button>
        <button className="btn-primary h-9 flex-1" onClick={onApplyNow}>
          Apply now
        </button>
      </div>
    </div>
  );
}

function Applying({ alloc }: { alloc: number }) {
  return (
    <div className="grid place-items-center py-16 px-6 text-center">
      <Loader2 className="h-7 w-7 animate-spin text-brand" />
      <p className="mt-4 text-sm font-medium text-fg">Reserving {formatBytes(alloc)} & restarting…</p>
      <p className="mt-1 text-xs text-fg-faint">Aperture will reconnect in a few seconds.</p>
    </div>
  );
}

function Legend({
  color,
  label,
  value,
  strong,
  ring,
}: {
  color: string;
  label: string;
  value: number;
  strong?: boolean;
  ring?: boolean;
}) {
  return (
    <div className="flex items-center gap-2 text-sm">
      <Swatch color={color} ring={ring} />
      <span className={`flex-1 ${strong ? "text-fg" : "text-fg-muted"}`}>{label}</span>
      <span className={`tabular-nums ${strong ? "text-fg font-medium" : "text-fg-muted"}`}>{formatBytes(value)}</span>
    </div>
  );
}

function SummaryRow({ label, value, strong, muted }: { label: string; value: string; strong?: boolean; muted?: boolean }) {
  return (
    <div className="flex items-center justify-between">
      <span className={muted ? "text-fg-faint" : "text-fg-muted"}>{label}</span>
      <span className={`tabular-nums ${strong ? "text-brand font-semibold" : muted ? "text-fg-faint" : "text-fg"}`}>
        {value}
      </span>
    </div>
  );
}
