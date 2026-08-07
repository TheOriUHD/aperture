import clsx from "clsx";

// Skeleton — a shared shimmer placeholder for loading states, so lists and grids
// reserve their layout instead of collapsing to a spinner (no jank, no layout
// shift when the data lands). Compose several to mirror the real content's shape.
export function Skeleton({ className }: { className?: string }) {
  return <div className={clsx("animate-pulse rounded-md bg-ink-700/70", className)} />;
}
