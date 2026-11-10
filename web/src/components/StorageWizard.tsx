import { useEffect, useState } from "react";
import { HardDrive, ShieldCheck, ChevronLeft, ChevronRight, Loader2, X, Check } from "lucide-react";
import { useDiskStats, useReserveStorage, type DiskStats } from "../hooks/useStorage";
import { useScrollLock } from "../hooks/useScrollLock";
import { formatBytes } from "../lib/format";

// Fills for the whole-disk bar and its legend: other programs' data (locked),
// the space reserved for Aperture, and what stays free.
const OTHER = "bg-fg-faint";
const APERTURE = "bg-brand";
const FREE = "bg-ink-600";
const GiB = 1024 ** 3;

const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));

// A horizontal stacked bar of the whole disk, with the share going to Aperture
// called out above it.
function DiskBar({ other, aperture, free }: { other: number; aperture: number; free: number }) {
  const total = other + aperture + free || 1;
  const pct = Math.round((aperture / total) * 100);
  const w = (v: number) => `${(v / total) * 100}%`;
  return (
    <div>
      <div className="flex items-baseline gap-2">
        <span className="text-3xl font-semibold tabular-nums text-fg">{pct}%</span>
        <span className="text-sm text-fg-muted">to Aperture</span>
      </div>
      <div className={`mt-3 flex h-2.5 w-full overflow-hidden rounded-full ${FREE}`}>
        <div className={`h-full ${OTHER}`} style={{ width: w(other) }} />
        <div className={`h-full border-l border-ink-800 ${APERTURE} transition-[width] duration-150`} style={{ width: w(aperture) }} />
      </div>
    </div>
  );
}

function Swatch({ fill }: { fill: string }) {
  return <span className={`h-2.5 w-2.5 shrink-0 rounded-[3px] ${fill}`} />;
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
      <div className="absolute inset-0 bg-black/60 animate-fade-in" onClick={applying ? undefined : onClose} />
      <div className="card relative w-[min(96vw,640px)] overflow-hidden shadow-pop animate-fade-in">
        {/* header / progress */}
        <div className="flex h-14 items-center gap-3 border-b border-line px-5">
          <HardDrive className="h-4 w-4 text-fg-muted" />
          <h2 className="text-base font-medium text-fg">Storage allocation</h2>
          <div className="ml-auto flex items-center gap-1.5">
            <span className="mr-1 text-xs tabular-nums text-fg-faint">Step {step} of 3</span>
            {[1, 2, 3].map((n) => (
              <span
                key={n}
                className={`h-1 rounded-full transition-all ${
                  n === step ? "w-5 bg-brand" : n < step ? "w-3 bg-brand/50" : "w-3 bg-ink-600"
                }`}
              />
            ))}
            {!applying && (
              <button onClick={onClose} className="btn-icon ml-2" aria-label="Close">
                <X className="h-4 w-4" />
              </button>
            )}
          </div>
        </div>

        {applying ? (
          <Applying alloc={alloc} />
        ) : !disk ? (
          <div className="grid place-items-center py-20">
            <Loader2 className="h-5 w-5 animate-spin text-brand" />
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
      <div className="mx-auto mb-4 grid h-12 w-12 place-items-center rounded-lg border border-line-strong bg-ink-850">
        <ShieldCheck className="h-6 w-6 text-brand" strokeWidth={1.75} />
      </div>
      <h3 className="text-center text-base font-semibold text-fg">Reserve space for Aperture</h3>
      <p className="mx-auto mt-2 max-w-md text-center text-sm text-fg-muted leading-relaxed">
        You’ll set how much of this {formatBytes(disk.totalBytes)} disk belongs to Aperture. That space is{" "}
        <span className="text-fg">hard-reserved</span> — physically held for recordings so no other program can take
        it, even while Aperture is off. Aperture stays within it automatically, trimming the oldest footage as it fills.
      </p>
      <ul className="mx-auto mt-5 max-w-md space-y-2.5 rounded-lg border border-line-strong p-4">
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
      <button className="btn-primary mt-6 w-full" onClick={onNext}>
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
      <div className="rounded-lg border border-line-strong p-4">
        <DiskBar other={disk.otherBytes} aperture={alloc} free={Math.max(0, free)} />
        <div className="mt-3 grid grid-cols-1 gap-x-6 gap-y-2 sm:grid-cols-3">
          <Legend fill={APERTURE} label="Aperture" value={alloc} strong />
          <Legend fill={FREE} label="Free" value={Math.max(0, free)} />
          <Legend fill={OTHER} label="Other data (locked)" value={disk.otherBytes} />
        </div>
        <div className="mt-3 flex justify-between border-t border-line-strong pt-2.5 text-xs text-fg-muted">
          <span>Whole disk</span>
          <span className="tabular-nums">{formatBytes(disk.totalBytes)}</span>
        </div>
      </div>

      <div className="mt-6">
        <div className="mb-2 flex items-end justify-between">
          <label className="label mb-0">Allocate to Aperture</label>
          <span className="text-lg font-semibold tabular-nums text-fg">{formatBytes(alloc)}</span>
        </div>
        <input
          type="range"
          min={GiB}
          max={maxAlloc}
          step={GiB / 2}
          value={alloc}
          onChange={(e) => setAlloc(clamp(Number(e.target.value), GiB, maxAlloc))}
          className="w-full"
          aria-label="Allocate to Aperture"
        />
        <div className="mt-1 flex justify-between text-[11px] text-fg-faint">
          <span>1 GB</span>
          <span>Max {formatBytes(maxAlloc)}</span>
        </div>
        {shrinksFootage ? (
          <p className="mt-2 text-xs text-warn">
            Below your current {formatBytes(disk.apertureUsedBytes)} of footage — the oldest recordings will be deleted
            to fit.
          </p>
        ) : (
          <p className="hint mt-2">
            {formatBytes(disk.marginBytes)} is always kept free so the disk can’t be filled completely.
          </p>
        )}
      </div>

      <div className="mt-6 flex gap-2">
        <button className="btn-ghost" onClick={onBack}>
          <ChevronLeft className="h-4 w-4" />
          Back
        </button>
        <button className="btn-primary flex-1" onClick={onNext}>
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
            <circle cx={size / 2} cy={size / 2} r={r} fill="none" className="stroke-ink-700" strokeWidth={6} />
            <circle
              cx={size / 2}
              cy={size / 2}
              r={r}
              fill="none"
              className="stroke-brand"
              strokeWidth={6}
              strokeLinecap="round"
              strokeDasharray={c}
              strokeDashoffset={c * (1 - frac)}
              style={{ transition: "stroke-dashoffset 1s linear" }}
            />
          </svg>
          <div className="absolute inset-0 grid place-items-center">
            <span className="text-3xl font-semibold tabular-nums text-fg">{seconds}</span>
          </div>
        </div>
        <p className="mt-3 text-center text-sm text-fg-muted">
          Applying and restarting Aperture in <span className="text-fg font-medium">{seconds}s</span>
        </p>
      </div>

      <div className="mt-5 space-y-2 rounded-lg border border-line-strong p-4 text-sm">
        <SummaryRow label="Hard-reserved for Aperture" value={formatBytes(alloc)} strong />
        <SummaryRow label="Free for other programs" value={formatBytes(free)} />
        <SummaryRow label="Other data (untouched)" value={formatBytes(disk.otherBytes)} />
        <div className="border-t border-line-strong pt-2">
          <SummaryRow label="Whole disk" value={formatBytes(disk.totalBytes)} muted />
        </div>
      </div>
      <p className="hint mt-2 text-center">
        The service restarts once to apply — live view and recording resume automatically.
      </p>

      <div className="mt-5 flex gap-2">
        <button className="btn-ghost" onClick={onBack}>
          <ChevronLeft className="h-4 w-4" />
          Back &amp; change
        </button>
        <button className="btn-primary flex-1" onClick={onApplyNow}>
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

function Legend({ fill, label, value, strong }: { fill: string; label: string; value: number; strong?: boolean }) {
  return (
    <div className="flex items-center gap-2 text-sm">
      <Swatch fill={fill} />
      <span className={`flex-1 ${strong ? "text-fg" : "text-fg-muted"}`}>{label}</span>
      <span className={`tabular-nums ${strong ? "font-medium text-fg" : "text-fg-muted"}`}>{formatBytes(value)}</span>
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
