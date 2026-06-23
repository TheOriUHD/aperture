import { Loader2 } from "lucide-react";

export function Spinner({ label }: { label?: string }) {
  return (
    <div className="flex items-center gap-2.5 text-fg-muted text-sm">
      <Loader2 className="h-4 w-4 animate-spin text-brand" />
      {label && <span>{label}</span>}
    </div>
  );
}
