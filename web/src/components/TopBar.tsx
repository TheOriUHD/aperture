import { useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { ChevronDown, LogOut, HardDrive, Aperture, Settings as SettingsIcon } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../lib/auth";
import { useStorage } from "../hooks/useStorage";
import { useSiteName, useSystemStatus } from "../hooks/useSystem";
import { useClickOutside } from "../hooks/useClickOutside";
import { formatBytes } from "../lib/format";
import { useHeaderSlots } from "./AppShell";

// TopBar is how a screen titles itself: it renders nothing in place, only the
// title (into the header's page tab) and any header actions (right side).
export function TopBar({ title, right }: { title: ReactNode; right?: ReactNode }) {
  const slots = useHeaderSlots();
  return (
    <>
      {slots.title && createPortal(title, slots.title)}
      {right && slots.right && createPortal(right, slots.right)}
    </>
  );
}

export function initialsOf(nameOrEmail: string): string {
  return (nameOrEmail || "?")
    .split(/[\s@.]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((s) => s[0]?.toUpperCase())
    .join("");
}

// The application header: site switcher on the left, the current page as a tab
// that opens into the page below it, the product mark centred, and storage plus
// the account menu on the right.
export function AppHeader({
  titleRef,
  rightRef,
}: {
  titleRef: (el: HTMLElement | null) => void;
  rightRef: (el: HTMLElement | null) => void;
}) {
  const navigate = useNavigate();
  const { user, logout } = useAuth();
  const { data: storage } = useStorage();
  const { data: site } = useSiteName();
  const { data: status } = useSystemStatus();
  const [menu, setMenu] = useState(false);
  const [siteMenu, setSiteMenu] = useState(false);
  const siteRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  useClickOutside(siteRef, () => setSiteMenu(false), siteMenu);
  useClickOutside(menuRef, () => setMenu(false), menu);

  const siteName = site?.siteName ?? "Home NVR";
  // Green unless something that should be running is down.
  const healthy = !status || (status.go2rtc.reachable && status.liveDownCount === 0 && status.recDownCount === 0);
  const cap = storage ? storage.totalAllocatedBytes ?? storage.allocatedBytes : 0;

  return (
    <header
      className="relative z-30 flex shrink-0 items-stretch bg-ink-700"
      style={{
        height: "calc(3rem + env(safe-area-inset-top))",
        paddingTop: "env(safe-area-inset-top)",
        paddingLeft: "env(safe-area-inset-left)",
        paddingRight: "max(0.75rem, env(safe-area-inset-right))",
      }}
    >
      {/* Site switcher */}
      <div ref={siteRef} className="relative flex items-center pl-3 lg:pl-4">
        <button
          onClick={() => setSiteMenu((m) => !m)}
          className="flex h-8 items-center gap-2 rounded-md px-2 text-[13px] font-medium text-fg hover:bg-white/[0.06]"
        >
          <span className={`h-1.5 w-1.5 rounded-full ${healthy ? "bg-online" : "bg-warn"}`} />
          <span className="max-w-[160px] truncate">{siteName}</span>
          <ChevronDown className="h-3.5 w-3.5 text-fg-muted" />
        </button>
        {siteMenu && (
          <div className="absolute left-3 top-full z-20 mt-1 w-64 animate-pop-in card py-1.5 shadow-pop">
            <div className="flex items-center gap-3 px-3 py-2">
              <span className="grid h-8 w-8 place-items-center rounded-md bg-brand-solid">
                <Aperture className="h-[18px] w-[18px] text-white" />
              </span>
              <div className="min-w-0">
                <p className="truncate text-sm font-medium text-fg">{siteName}</p>
                <p className="text-xs text-fg-faint">Aperture NVR{status?.version ? ` · ${status.version}` : ""}</p>
              </div>
            </div>
            {user?.role === "admin" && (
              <button
                onClick={() => {
                  setSiteMenu(false);
                  navigate("/settings?tab=general");
                }}
                className="flex w-full items-center gap-2.5 px-3 py-2 text-sm text-fg-muted hover:bg-white/[0.04] hover:text-fg"
              >
                <SettingsIcon className="h-4 w-4" />
                Site settings
              </button>
            )}
          </div>
        )}
      </div>

      {/* The page tab: same colour as the page, so it reads as opening into it. */}
      <div className="ml-2 flex min-w-0 items-center gap-2 bg-ink-900 px-4 text-[13px] font-medium text-brand lg:ml-3 lg:px-5">
        <Aperture className="h-4 w-4 shrink-0" />
        <span ref={titleRef} className="truncate" />
      </div>

      {/* Product mark */}
      <div className="pointer-events-none absolute left-1/2 top-[env(safe-area-inset-top)] hidden h-12 -translate-x-1/2 items-center md:flex">
        <span className="text-[15px] font-semibold tracking-[0.02em] text-fg/40">Aperture</span>
      </div>

      <div className="ml-auto flex items-center gap-2 pl-3">
        <div ref={rightRef} className="flex items-center gap-2" />
        {storage && (
          <button
            onClick={() => user?.role === "admin" && navigate("/settings?tab=storage")}
            className="hidden h-8 items-center gap-1.5 rounded-md px-2 text-xs text-fg-muted hover:bg-white/[0.06] hover:text-fg sm:flex"
            title="Storage"
          >
            <HardDrive className="h-3.5 w-3.5" />
            <span className="tabular-nums">
              {/* Total across every destination (local + any network share), not the
                  local allocation — usedBytes already counts all locations. */}
              {cap > 0 ? `${formatBytes(storage.usedBytes)} / ${formatBytes(cap)}` : formatBytes(storage.usedBytes)}
            </span>
          </button>
        )}
        <div ref={menuRef} className="relative">
          <button
            onClick={() => setMenu((m) => !m)}
            className="grid h-7 w-7 place-items-center rounded-full bg-brand-solid text-[11px] font-semibold text-white hover:bg-brand-hover"
            aria-label="Account menu"
          >
            {initialsOf(user?.name || user?.email || "?")}
          </button>
          {menu && (
            <div className="absolute right-0 z-20 mt-2 w-60 animate-pop-in card py-1.5 shadow-pop">
              <div className="flex items-center gap-3 border-b border-line px-3 pb-2.5 pt-1.5">
                <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-brand-solid text-xs font-semibold text-white">
                  {initialsOf(user?.name || user?.email || "?")}
                </span>
                <div className="min-w-0">
                  <p className="truncate text-sm text-fg">{user?.name || user?.email}</p>
                  <p className="text-xs capitalize text-fg-faint">{user?.role}</p>
                </div>
              </div>
              <button
                onClick={() => {
                  setMenu(false);
                  navigate("/settings?tab=general");
                }}
                className="mt-1 flex w-full items-center gap-2.5 px-3 py-2 text-sm text-fg-muted hover:bg-white/[0.04] hover:text-fg"
              >
                <SettingsIcon className="h-4 w-4" />
                Account &amp; preferences
              </button>
              <button
                onClick={() => void logout()}
                className="flex w-full items-center gap-2.5 px-3 py-2 text-sm text-fg-muted hover:bg-white/[0.04] hover:text-fg"
              >
                <LogOut className="h-4 w-4" />
                Sign out
              </button>
            </div>
          )}
        </div>
      </div>
    </header>
  );
}
