// Layout primitives shared by the list views (devices, logs, users, alarms) and
// the detail views (camera side panel, settings). They encode the layout the app
// is styled after: a collapsible filter sidebar on the left, a full-width table,
// a side panel sliding in from the right, collapsible filled sections inside it,
// and two-column label/control rows in settings.
import { useState, type ReactNode } from "react";
import clsx from "clsx";
import {
  ChevronDown,
  ChevronUp,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  SlidersHorizontal,
  X,
  type LucideIcon,
} from "lucide-react";
import { useEscapeStack } from "../../lib/escapeStack";
import { useMediaQuery } from "../../hooks/useMediaQuery";

// ---- Page with a filter sidebar ----------------------------------------------

// SidebarPage lays out a left filter sidebar beside the page's main content. The
// sidebar collapses to nothing on desktop (the toggle stays on its edge); on a
// phone it opens as a full-screen sheet from a "Filters" button.
export function SidebarPage({
  sidebar,
  children,
  storageKey,
  width = 280,
}: {
  sidebar: ReactNode;
  children: ReactNode;
  storageKey?: string;
  width?: number;
}) {
  const desktop = useMediaQuery("(min-width: 1024px)");
  const [open, setOpen] = useState(() => {
    if (!storageKey) return true;
    try {
      return localStorage.getItem(`aperture.sidebar.${storageKey}`) !== "closed";
    } catch {
      return true;
    }
  });
  const toggle = () =>
    setOpen((o) => {
      try {
        if (storageKey) localStorage.setItem(`aperture.sidebar.${storageKey}`, o ? "closed" : "open");
      } catch {
        /* private mode: keep it in memory only */
      }
      return !o;
    });

  if (!desktop) return <MobileSidebarPage sidebar={sidebar}>{children}</MobileSidebarPage>;
  return (
    <div className="flex min-h-0 flex-1">
      <aside
        className={clsx(
          "relative shrink-0 border-r border-line transition-[width] duration-200",
          open ? "" : "w-0 border-r-0",
        )}
        style={open ? { width } : undefined}
      >
        <div className={clsx("h-full overflow-y-auto overflow-x-hidden px-3.5 py-3", !open && "hidden")}>{sidebar}</div>
        <button
          onClick={toggle}
          title={open ? "Hide filters" : "Show filters"}
          aria-label={open ? "Hide filters" : "Show filters"}
          className="absolute -right-3 top-3 z-20 grid h-7 w-6 place-items-center rounded-md border border-line bg-ink-900 text-fg-faint hover:text-fg"
        >
          {open ? <PanelLeftClose className="h-3.5 w-3.5" /> : <PanelLeftOpen className="h-3.5 w-3.5" />}
        </button>
      </aside>
      <div className="relative flex min-w-0 flex-1 flex-col overflow-hidden">{children}</div>
    </div>
  );
}

function MobileSidebarPage({ sidebar, children }: { sidebar: ReactNode; children: ReactNode }) {
  const [sheet, setSheet] = useState(false);
  useEscapeStack(sheet, () => setSheet(false));
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b border-line px-4 py-2">
        <button className="btn-ghost" onClick={() => setSheet(true)}>
          <SlidersHorizontal className="h-4 w-4" />
          Filters
        </button>
      </div>
      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">{children}</div>
      {sheet && (
        <div className="fixed inset-0 z-50 flex animate-fade-in flex-col bg-ink-900">
          <div
            className="flex h-12 shrink-0 items-center justify-between border-b border-line px-4"
            style={{ paddingTop: "env(safe-area-inset-top)" }}
          >
            <span className="text-sm font-semibold text-fg">Filters</span>
            <button className="btn-link" onClick={() => setSheet(false)}>
              Done
            </button>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3" style={{ paddingBottom: "max(1rem, env(safe-area-inset-bottom))" }}>
            {sidebar}
          </div>
        </div>
      )}
    </div>
  );
}

// SidebarSearch is the search field at the top of a filter sidebar.
export function SidebarSearch({
  value,
  onChange,
  placeholder = "Search",
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
}) {
  return (
    <div className="relative">
      <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-faint" />
      <input
        className="input pl-9"
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        aria-label={placeholder}
      />
      {value && (
        <button
          onClick={() => onChange("")}
          className="absolute right-2 top-1/2 grid h-5 w-5 -translate-y-1/2 place-items-center rounded text-fg-faint hover:text-fg"
          aria-label="Clear search"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  );
}

// SidebarSection is a collapsible group of filters with a bold title.
export function SidebarSection({
  title,
  children,
  defaultOpen = true,
  action,
}: {
  title: string;
  children: ReactNode;
  defaultOpen?: boolean;
  action?: ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <section className="border-b border-line py-3 last:border-b-0">
      <div className="flex items-center gap-2">
        <button
          onClick={() => setOpen((o) => !o)}
          className="flex flex-1 items-center justify-between text-left text-[13px] font-semibold text-fg"
        >
          {title}
          {open ? <ChevronUp className="h-4 w-4 text-fg-faint" /> : <ChevronDown className="h-4 w-4 text-fg-faint" />}
        </button>
        {action}
      </div>
      {open && <div className="mt-2.5">{children}</div>}
    </section>
  );
}

// CheckRow is one checkbox filter: label, optional icon and count.
export function CheckRow({
  checked,
  onChange,
  label,
  count,
  icon: Icon,
  disabled,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: ReactNode;
  count?: number;
  icon?: LucideIcon;
  disabled?: boolean;
}) {
  return (
    <label
      className={clsx(
        "flex h-8 cursor-pointer items-center gap-2.5 rounded-md px-1 text-sm text-fg/90 hover:bg-white/[0.03]",
        disabled && "pointer-events-none opacity-40",
      )}
    >
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} disabled={disabled} />
      {Icon && <Icon className="h-4 w-4 shrink-0 text-fg-muted" />}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {count !== undefined && <span className="tabular-nums text-xs text-fg-faint">{count}</span>}
    </label>
  );
}

// ---- Segmented control ---------------------------------------------------------

export function Segmented<T extends string>({
  value,
  onChange,
  options,
  className,
  full,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: ReactNode; title?: string; disabled?: boolean }[];
  className?: string;
  full?: boolean;
}) {
  return (
    <div className={clsx("seg", full && "flex w-full", className)} role="tablist">
      {options.map((o) => (
        <button
          key={o.value}
          role="tab"
          aria-selected={value === o.value}
          title={o.title}
          disabled={o.disabled}
          onClick={() => onChange(o.value)}
          className={clsx("seg-item", full && "flex-1", value === o.value && "seg-item-active", o.disabled && "cursor-not-allowed opacity-40")}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

// ---- Side panel ----------------------------------------------------------------

export interface PanelTab {
  id: string;
  icon: LucideIcon;
  label: string;
}

// SidePanel slides in over the right edge of the page content: a header with the
// item's name, icon tabs, and a scrolling body of filled sections. On a phone it
// takes the whole screen.
export function SidePanel({
  open,
  onClose,
  title,
  subtitle,
  tabs,
  tab,
  onTab,
  children,
  footer,
  wide,
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  subtitle?: ReactNode;
  tabs?: PanelTab[];
  tab?: string;
  onTab?: (id: string) => void;
  children: ReactNode;
  footer?: ReactNode;
  wide?: boolean;
}) {
  useEscapeStack(open, onClose);
  if (!open) return null;
  return (
    <aside
      className={clsx(
        "fixed inset-0 z-40 flex animate-slide-in-right flex-col bg-ink-900",
        "lg:absolute lg:inset-y-0 lg:left-auto lg:right-0 lg:z-20 lg:max-w-full lg:border-l lg:border-line lg:shadow-drawer",
        wide ? "lg:w-[560px]" : "lg:w-[440px]",
      )}
    >
      <div
        className="flex h-12 shrink-0 items-center gap-2 px-4"
        style={{ paddingTop: "env(safe-area-inset-top)" }}
      >
        <div className="min-w-0 flex-1 text-center">
          <span className="truncate text-sm font-semibold text-fg">{title}</span>
          {subtitle && <span className="ml-2 truncate text-sm text-fg-muted">{subtitle}</span>}
        </div>
        <button className="btn-icon -mr-1.5" onClick={onClose} aria-label="Close">
          <X className="h-4 w-4" />
        </button>
      </div>
      {tabs && tabs.length > 1 && (
        <div className="mx-4 mb-3 grid shrink-0 rounded-md bg-ink-800 p-1" style={{ gridTemplateColumns: `repeat(${tabs.length}, 1fr)` }}>
          {tabs.map((t) => (
            <button
              key={t.id}
              onClick={() => onTab?.(t.id)}
              title={t.label}
              aria-label={t.label}
              className={clsx(
                "grid h-8 place-items-center rounded transition-colors",
                tab === t.id ? "bg-ink-700 text-brand" : "text-fg-muted hover:text-fg",
              )}
            >
              <t.icon className="h-[18px] w-[18px]" />
            </button>
          ))}
        </div>
      )}
      <div data-panel-body className="min-h-0 flex-1 space-y-3 overflow-y-auto px-4 pb-4">
        {children}
      </div>
      {footer && <div className="shrink-0 border-t border-line bg-ink-900 px-4 py-3">{footer}</div>}
    </aside>
  );
}

// DetailSection is a filled, optionally collapsible block inside a side panel.
export function DetailSection({
  title,
  children,
  collapsible = true,
  defaultOpen = true,
  className,
  bodyClass,
}: {
  title?: ReactNode;
  children: ReactNode;
  collapsible?: boolean;
  defaultOpen?: boolean;
  className?: string;
  bodyClass?: string;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <section className={clsx("rounded-lg bg-ink-800", className)}>
      {title && (
        <button
          type="button"
          disabled={!collapsible}
          onClick={() => setOpen((o) => !o)}
          className="flex w-full items-center justify-between px-4 py-3.5 text-left text-sm font-semibold text-fg disabled:cursor-default"
        >
          {title}
          {collapsible &&
            (open ? <ChevronUp className="h-4 w-4 text-fg-muted" /> : <ChevronDown className="h-4 w-4 text-fg-muted" />)}
        </button>
      )}
      {(open || !collapsible) && <div className={clsx("px-4 pb-4", !title && "pt-4", bodyClass)}>{children}</div>}
    </section>
  );
}

// InfoList renders label/value rows (the "Connection · Excellent" list).
export function InfoList({ rows }: { rows: { label: ReactNode; value: ReactNode; tone?: "ok" | "warn" | "bad" }[] }) {
  return (
    <dl className="space-y-3 text-sm">
      {rows.map((r, i) => (
        <div key={i} className="flex items-center justify-between gap-4">
          <dt className="text-fg/85">{r.label}</dt>
          <dd
            className={clsx(
              "min-w-0 truncate text-right tabular-nums",
              r.tone === "ok" ? "text-online" : r.tone === "warn" ? "text-warn" : r.tone === "bad" ? "text-danger" : "text-fg/85",
            )}
          >
            {r.value}
          </dd>
        </div>
      ))}
    </dl>
  );
}

// ---- Settings rows -------------------------------------------------------------

// SettingsRow is the two-column settings line: a label (with optional hint) on the
// left, its control on the right. Stacks on a phone.
export function SettingsRow({
  label,
  hint,
  children,
  align = "center",
}: {
  label: ReactNode;
  hint?: ReactNode;
  children: ReactNode;
  align?: "center" | "start";
}) {
  return (
    <div
      className={clsx(
        "grid gap-x-8 gap-y-2 py-3 md:grid-cols-[minmax(180px,260px)_1fr]",
        align === "center" ? "md:items-center" : "md:items-start",
      )}
    >
      <div className={clsx(align === "start" && "md:pt-1.5")}>
        <div className="text-sm text-fg-muted">{label}</div>
        {hint && <div className="mt-0.5 text-xs leading-relaxed text-fg-faint">{hint}</div>}
      </div>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

// SettingsHeading starts a group of rows ("Advanced").
export function SettingsHeading({ children, description }: { children: ReactNode; description?: ReactNode }) {
  return (
    <div className="pb-1 pt-6 first:pt-0">
      <h3 className="text-sm font-semibold text-fg">{children}</h3>
      {description && <p className="mt-1 text-xs leading-relaxed text-fg-faint">{description}</p>}
    </div>
  );
}

// ---- Small pieces --------------------------------------------------------------

export function Avatar({ name, className }: { name: string; className?: string }) {
  const initials = (name || "?")
    .split(/[\s@.]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((s) => s[0]?.toUpperCase())
    .join("");
  return (
    <span
      className={clsx(
        "grid h-6 w-6 shrink-0 place-items-center rounded bg-brand-solid text-[10px] font-semibold text-white",
        className,
      )}
    >
      {initials}
    </span>
  );
}

// SeverityBars draws the four-step severity meter used in logs.
export function SeverityBars({ level }: { level: 1 | 2 | 3 | 4 }) {
  const color = level === 1 ? "bg-online" : level === 4 ? "bg-danger" : "bg-warn";
  return (
    <span className="inline-flex items-center gap-[3px]" aria-label={`Severity ${level} of 4`}>
      {[1, 2, 3, 4].map((i) => (
        <span key={i} className={clsx("h-[3px] w-2.5 rounded-full", i <= level ? color : "bg-[#3a3e43]")} />
      ))}
    </span>
  );
}

// StatusDot is the small coloured dot in front of a status word.
export function StatusDot({ tone }: { tone: "ok" | "warn" | "bad" | "off" }) {
  return (
    <span
      className={clsx(
        "inline-block h-2 w-2 shrink-0 rounded-full",
        tone === "ok" ? "bg-online" : tone === "warn" ? "bg-warn" : tone === "bad" ? "bg-danger" : "bg-fg-faint",
      )}
    />
  );
}
