import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

export function EmptyState({
  icon: Icon,
  title,
  hint,
  action,
}: {
  icon: LucideIcon;
  title: string;
  hint?: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center justify-center text-center py-16 px-6">
      <div className="h-12 w-12 rounded-xl bg-ink-700 grid place-items-center mb-4">
        <Icon className="h-6 w-6 text-fg-faint" />
      </div>
      <p className="text-fg font-medium">{title}</p>
      {hint && <p className="text-sm text-fg-muted mt-1 max-w-sm">{hint}</p>}
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}
