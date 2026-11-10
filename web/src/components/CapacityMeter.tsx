import clsx from "clsx";
import { TIERS, TIER_LABEL, type Capacity, type Tier } from "../lib/capacity";

export const TIER_STYLE: Record<Tier, { bar: string; chip: string }> = {
  HD: { bar: "bg-online", chip: "bg-online/15 text-online" },
  "2K": { bar: "bg-brand", chip: "bg-brand/15 text-brand" },
  "4K": { bar: "bg-[#a78bfa]", chip: "bg-[#a78bfa]/15 text-[#c4b5fd]" },
  "4K+": { bar: "bg-[#f472b6]", chip: "bg-[#f472b6]/15 text-[#f9a8d4]" },
};

export const pctText = (p: number) => `${p > 0 && p < 0.01 ? "<1" : Math.round(p * 100)}%`;

// CapacityBar: one segment per camera, as wide as its share of the budget that
// limits this machine, coloured by its resolution tier. Over capacity the segments
// are scaled to fit and the bar turns red at the end.
export function CapacityBar({ capacity, className }: { capacity: Capacity; className?: string }) {
  const over = capacity.pct > 1;
  const scale = over ? 1 / capacity.pct : 1;
  return (
    <div
      className={clsx("flex h-2 w-full gap-px overflow-hidden rounded-full bg-ink-700", over && "ring-1 ring-danger/70", className)}
      role="meter"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(capacity.pct * 100)}
      aria-label="Camera capacity used"
    >
      {capacity.cameras.map((c) => {
        const width = Math.max(0, c.share * scale * 100);
        if (width <= 0) return null;
        return (
          <div
            key={c.id}
            className={clsx("h-full first:rounded-l-full", TIER_STYLE[c.tier].bar, c.estimated && "opacity-60")}
            style={{ width: `${width}%`, minWidth: 2 }}
            title={`${c.name} · ${c.tier} · ${pctText(c.share)}${c.notes.length ? ` · ${c.notes.join(" · ")}` : ""}`}
          />
        );
      })}
      {over && <div className="ml-auto h-full w-1.5 shrink-0 bg-danger" />}
    </div>
  );
}

export function TierChips({ capacity, all }: { capacity: Capacity; all?: boolean }) {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
      {TIERS.filter((t) => all || capacity.tiers[t] > 0 || t !== "4K+").map((t) => (
        <div key={t} className="flex items-center gap-1.5 text-sm">
          <span className={clsx("rounded px-1.5 py-0.5 text-[11px] font-semibold", TIER_STYLE[t].chip)}>{TIER_LABEL[t]}</span>
          <span className="tabular-nums text-fg-muted">×{capacity.tiers[t]}</span>
        </div>
      ))}
    </div>
  );
}
