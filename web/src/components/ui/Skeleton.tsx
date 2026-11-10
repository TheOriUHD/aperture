import clsx from "clsx";

// Skeleton — a shared shimmer placeholder for loading states, so lists and grids
// reserve their layout instead of collapsing to a spinner (no jank, no layout
// shift when the data lands). Compose several to mirror the real content's shape.
// Translucent, so one placeholder reads the same on the page and inside a filled
// panel or card.
export function Skeleton({ className }: { className?: string }) {
  return <div aria-hidden className={clsx("animate-pulse rounded-md bg-white/[0.06]", className)} />;
}
