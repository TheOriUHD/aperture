// Spinner is the thin accent ring used for inline and screen-level loading.
export function Spinner({ label }: { label?: string }) {
  return (
    <div className="flex items-center gap-2.5 text-sm text-fg-muted" role="status" aria-live="polite">
      <span
        aria-hidden
        className="h-4 w-4 shrink-0 animate-spin rounded-full border-2 border-brand/25 border-t-brand"
      />
      {label ? <span>{label}</span> : <span className="sr-only">Loading</span>}
    </div>
  );
}
