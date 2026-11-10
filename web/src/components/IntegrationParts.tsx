import type { ReactNode } from "react";
import clsx from "clsx";
import { Check, Copy, type LucideIcon } from "lucide-react";

// Shared building blocks for the integration pages. Each integration is shown on
// its own in the Integrations screen's content column, so it reads as a page
// section rather than a boxed card: a header row (icon, title, status on the
// left; the section's action on the right), a short description, then settings
// rows and tables.

export function IntegrationHeader({
  icon: Icon,
  title,
  status,
  action,
  children,
}: {
  icon: LucideIcon;
  title: ReactNode;
  status?: ReactNode;
  action?: ReactNode;
  /** The one-line description under the title. */
  children?: ReactNode;
}) {
  return (
    <header className="pb-3">
      <div className="flex min-h-8 flex-wrap items-center gap-x-2.5 gap-y-2">
        <Icon className="h-[18px] w-[18px] shrink-0 text-fg-muted" strokeWidth={1.75} />
        <h2 className="text-[15px] font-semibold text-fg">{title}</h2>
        {status}
        {action && <div className="ml-auto flex shrink-0 items-center gap-2">{action}</div>}
      </div>
      {children && (
        <p className="mt-1.5 max-w-[760px] text-sm leading-relaxed text-fg-muted [&_code]:text-[13px] [&_code]:text-fg/90">{children}</p>
      )}
    </header>
  );
}

const PILL = { ok: "pill-green", warn: "pill-orange", bad: "pill-red", off: "pill-gray", info: "pill-blue" } as const;

// StatusPill is a small rectangular state badge with a leading dot.
export function StatusPill({ tone, children }: { tone: keyof typeof PILL; children: ReactNode }) {
  return (
    <span className={PILL[tone]}>
      <span className="h-1.5 w-1.5 rounded-full bg-current" />
      {children}
    </span>
  );
}

const CALLOUT = {
  warn: ["border-warn/30 bg-warn/[0.08]", "text-warn"],
  danger: ["border-danger/30 bg-danger/[0.08]", "text-danger"],
  ok: ["border-online/30 bg-online/[0.08]", "text-online"],
  info: ["border-brand/30 bg-brand/[0.08]", "text-brand"],
} as const;

// Callout is a tinted, outlined note: a warning, an error, a one-time secret.
export function Callout({
  tone,
  icon: Icon,
  children,
  className,
}: {
  tone: keyof typeof CALLOUT;
  icon?: LucideIcon;
  children: ReactNode;
  className?: string;
}) {
  const [box, ink] = CALLOUT[tone];
  return (
    <div className={clsx("flex items-start gap-2.5 rounded-md border px-3 py-2.5 text-xs leading-relaxed text-fg-muted", box, className)}>
      {Icon && <Icon className={clsx("mt-px h-4 w-4 shrink-0", ink)} />}
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

// CodeValue shows a read-only value (a URL, a key) in a field-shaped box.
export function CodeValue({ children, className, title }: { children: ReactNode; className?: string; title?: string }) {
  return (
    <code
      title={title}
      className={clsx(
        "block h-8 min-w-0 truncate rounded-md border border-white/[0.07] bg-white/[0.03] px-3 font-mono text-xs leading-[30px] text-fg/85",
        className,
      )}
    >
      {children}
    </code>
  );
}

// CopyIconButton is the square copy action beside a value; the caller owns the
// clipboard write and the "copied" flag.
export function CopyIconButton({
  copied,
  onClick,
  label,
  className,
}: {
  copied: boolean;
  onClick: () => void;
  label: string;
  className?: string;
}) {
  return (
    <button type="button" className={clsx("btn-icon shrink-0", className)} onClick={onClick} aria-label={label} title={label}>
      {copied ? <Check className="h-4 w-4 text-online" /> : <Copy className="h-4 w-4" />}
    </button>
  );
}

// SectionTitle starts a sub-section that carries its own action on the right
// (SettingsHeading has no action slot).
export function SectionTitle({
  children,
  description,
  action,
}: {
  children: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-end gap-x-4 gap-y-2 pb-2 pt-8">
      <div className="min-w-0 flex-1">
        <h3 className="text-sm font-semibold text-fg">{children}</h3>
        {description && <p className="mt-1 max-w-[760px] text-xs leading-relaxed text-fg-faint">{description}</p>}
      </div>
      {action && <div className="flex shrink-0 items-center gap-2">{action}</div>}
    </div>
  );
}

// ToggleRow is a SettingsRow for a switch: the same two columns on a wide screen,
// but on a phone the switch stays on the label's line instead of dropping below it.
export function ToggleRow({ label, hint, children }: { label: ReactNode; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4 py-3 md:grid md:grid-cols-[minmax(180px,260px)_1fr] md:gap-x-8">
      <div className="min-w-0">
        <div className="text-sm text-fg-muted">{label}</div>
        {hint && <div className="mt-0.5 text-xs leading-relaxed text-fg-faint">{hint}</div>}
      </div>
      <div className="flex shrink-0">{children}</div>
    </div>
  );
}
