import { NavLink } from "react-router-dom";
import { useTranslation } from "react-i18next";
import clsx from "clsx";
import {
  LayoutGrid,
  Play,
  Bell,
  Settings,
  Puzzle,
  Cctv,
  ScrollText,
  Workflow,
  Plug,
  Users,
  type LucideIcon,
} from "lucide-react";
import { useExtNav } from "../ext/useExtNav";
import type { ExtNavItem } from "../ext/types";
import { useAuth } from "../lib/auth";
import { useUnreadAlarms } from "../lib/unreadAlarms";

interface NavItem {
  to: string;
  tkey: string;
  label: string;
  icon: LucideIcon;
  admin?: boolean;
}

// The views you work in, top of the rail.
const primary: NavItem[] = [
  { to: "/dashboard", tkey: "nav.live", label: "Dashboard", icon: LayoutGrid },
  { to: "/cameras", tkey: "nav.devices", label: "Devices", icon: Cctv },
  { to: "/playback", tkey: "nav.playback", label: "Playback", icon: Play },
  { to: "/alarms", tkey: "nav.alarms", label: "Alarms", icon: Bell },
];
// Administration, bottom of the rail.
const admin: NavItem[] = [
  { to: "/settings", tkey: "nav.settings", label: "Settings", icon: Settings },
  { to: "/logs", tkey: "nav.logs", label: "Activity Log", icon: ScrollText, admin: true },
  { to: "/automations", tkey: "nav.automations", label: "Automations", icon: Workflow, admin: true },
  { to: "/integrations", tkey: "nav.integrations", label: "Integrations", icon: Plug, admin: true },
  { to: "/users", tkey: "nav.users", label: "Users", icon: Users, admin: true },
];
// Mobile keeps four big targets; the rest are reachable from Settings.
const mobile: NavItem[] = [
  { to: "/dashboard", tkey: "nav.live", label: "Live", icon: LayoutGrid },
  { to: "/playback", tkey: "nav.playback", label: "Playback", icon: Play },
  { to: "/alarms", tkey: "nav.alarms", label: "Alarms", icon: Bell },
  { to: "/settings", tkey: "nav.settings", label: "Settings", icon: Settings },
];

// Desktop: a vertical icon rail under the header. Hidden on mobile.
export function NavRail() {
  const { t } = useTranslation();
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const { count } = useUnreadAlarms();
  // Pinned extensions each earn their own rail entry + full page (user-granted;
  // with none pinned the rail is as before). They follow the primary views and
  // sit above the administration group.
  const { data: pinned = [] } = useExtNav();
  return (
    <nav className="hidden w-14 shrink-0 flex-col items-center gap-2 border-r border-line bg-ink-900 py-3 lg:flex">
      {primary.map((item) => (
        <RailLink
          key={item.to}
          to={item.to}
          icon={item.icon}
          label={t(item.tkey, item.label)}
          badge={item.to === "/alarms" ? count : 0}
        />
      ))}
      {pinned.length > 0 && <div className="my-1 h-px w-6 bg-line" />}
      {pinned.map((ext) => (
        <ExtRailLink key={ext.id} ext={ext} />
      ))}
      <div className="mt-auto flex flex-col items-center gap-2">
        <div className="mb-1 h-px w-6 bg-line" />
        {admin
          .filter((item) => !item.admin || isAdmin)
          .map((item) => (
            <RailLink key={item.to} to={item.to} icon={item.icon} label={t(item.tkey, item.label)} />
          ))}
      </div>
    </nav>
  );
}

// ExtRailLink renders a pinned extension's rail entry. The icon is loaded as an
// <img> from the extension's CSP'd asset route — NEVER inlined as markup — so a
// hostile bundle can't run script in the host session via the rail; a missing or
// broken icon falls back to the neutral Puzzle glyph.
function ExtRailLink({ ext }: { ext: ExtNavItem }) {
  return (
    <NavLink
      to={`/extensions/${ext.id}`}
      aria-label={ext.name}
      className={({ isActive }) => clsx("nav-item group", isActive && "nav-item-active")}
    >
      {ext.icon ? <img src={ext.icon} alt="" className="h-5 w-5 rounded object-cover" /> : <Puzzle className="h-5 w-5" />}
      <RailTip label={ext.name} />
    </NavLink>
  );
}

// Mobile: a bottom tab bar. Hidden on desktop.
export function BottomNav() {
  const { t } = useTranslation();
  const { count } = useUnreadAlarms();
  return (
    <nav
      className="flex shrink-0 items-stretch justify-around border-t border-line bg-ink-900 lg:hidden"
      style={{
        // Match the OS's background_color (ink-900): the iOS PWA leaves a ~62px
        // home-indicator strip below the visible viewport that the web view can't
        // paint. Using the same colour as that strip makes the tab bar blend into
        // it instead of reading as a lighter bar with a dark strip beneath.
        height: "calc(3.25rem + env(safe-area-inset-bottom))",
        paddingBottom: "env(safe-area-inset-bottom)",
      }}
    >
      {mobile.map((item) => {
        const label = t(item.tkey, item.label);
        return (
          <NavLink
            key={item.to}
            to={item.to}
            aria-label={label}
            className={({ isActive }) =>
              clsx(
                "relative flex flex-1 flex-col items-center justify-center gap-0.5",
                isActive ? "text-brand" : "text-fg-faint",
              )
            }
          >
            <span className="relative">
              <item.icon className="h-5 w-5" />
              {item.to === "/alarms" && count > 0 && <Badge count={count} />}
            </span>
            <span className="text-[10px]">{label}</span>
          </NavLink>
        );
      })}
    </nav>
  );
}

function RailLink({ to, label, icon: Icon, badge = 0 }: { to: string; label: string; icon: LucideIcon; badge?: number }) {
  return (
    <NavLink
      to={to}
      aria-label={label}
      className={({ isActive }) => clsx("nav-item group", isActive && "nav-item-active")}
    >
      <Icon className="h-[19px] w-[19px]" strokeWidth={1.75} />
      {badge > 0 && <Badge count={badge} />}
      <RailTip label={label} />
    </NavLink>
  );
}

function Badge({ count }: { count: number }) {
  return (
    <span className="absolute -right-1.5 -top-1.5 grid h-4 min-w-4 place-items-center rounded-full bg-rec px-1 text-[10px] font-semibold leading-none text-white ring-2 ring-ink-900">
      {count > 9 ? "9+" : count}
    </span>
  );
}

// A label that slides out to the right of a rail icon on hover.
function RailTip({ label }: { label: string }) {
  return (
    <span className="pointer-events-none absolute left-full top-1/2 z-50 ml-3 hidden -translate-y-1/2 whitespace-nowrap rounded-md bg-ink-700 px-2.5 py-1.5 text-xs font-medium text-fg shadow-pop group-hover:block">
      {label}
    </span>
  );
}
