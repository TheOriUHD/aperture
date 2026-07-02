import { Link, NavLink } from "react-router-dom";
import { useTranslation } from "react-i18next";
import clsx from "clsx";
import {
  LayoutGrid,
  Cctv,
  Clapperboard,
  Bell,
  Settings,
  Aperture,
  type LucideIcon,
} from "lucide-react";

interface NavItem {
  to: string;
  tkey: string;
  label: string;
  icon: LucideIcon;
}

const items: NavItem[] = [
  { to: "/dashboard", tkey: "nav.live", label: "Live", icon: LayoutGrid },
  { to: "/cameras", tkey: "nav.cameras", label: "Cameras", icon: Cctv },
  { to: "/playback", tkey: "nav.playback", label: "Playback", icon: Clapperboard },
  { to: "/alarms", tkey: "nav.alarms", label: "Alarms", icon: Bell },
];
const settingsItem: NavItem = { to: "/settings", tkey: "nav.settings", label: "Settings", icon: Settings };

// Desktop: a vertical icon rail on the left. Hidden on mobile.
export function NavRail() {
  const { t } = useTranslation();
  return (
    <nav className="hidden lg:flex flex-col items-center gap-1.5 w-[54px] shrink-0 bg-ink-850 border-r border-line py-3">
      <Link
        to="/dashboard"
        title="Aperture — home"
        aria-label="Home"
        className="h-8 w-8 rounded-lg bg-brand grid place-items-center mb-3 hover:bg-[#2aa6f5] transition-colors"
      >
        <Aperture className="h-5 w-5 text-white" />
      </Link>
      {items.map((item) => (
        <RailLink key={item.to} to={item.to} icon={item.icon} label={t(item.tkey, item.label)} />
      ))}
      <div className="mt-auto">
        <RailLink to={settingsItem.to} icon={settingsItem.icon} label={t(settingsItem.tkey, settingsItem.label)} />
      </div>
    </nav>
  );
}

// Mobile: a bottom tab bar. Hidden on desktop.
export function BottomNav() {
  const { t } = useTranslation();
  const all = [...items, settingsItem];
  return (
    <nav
      className="flex lg:hidden items-stretch justify-around shrink-0 border-t border-line bg-ink-850"
      style={{
        height: "calc(3.5rem + env(safe-area-inset-bottom))",
        paddingBottom: "env(safe-area-inset-bottom)",
      }}
    >
      {all.map((item) => {
        const label = t(item.tkey, item.label);
        return (
          <NavLink
            key={item.to}
            to={item.to}
            aria-label={label}
            className={({ isActive }) =>
              clsx(
                "flex flex-1 flex-col items-center justify-center gap-0.5",
                isActive ? "text-brand" : "text-fg-faint",
              )
            }
          >
            <item.icon className="h-5 w-5" />
            <span className="text-[10px]">{label}</span>
          </NavLink>
        );
      })}
    </nav>
  );
}

function RailLink({ to, label, icon: Icon }: { to: string; label: string; icon: LucideIcon }) {
  return (
    <NavLink
      to={to}
      title={label}
      aria-label={label}
      className={({ isActive }) => clsx("nav-item group", isActive && "nav-item-active")}
    >
      {({ isActive }) => (
        <>
          <Icon className="h-5 w-5" />
          {isActive && (
            <span className="absolute left-0 top-1/2 -translate-y-1/2 h-5 w-0.5 rounded-r bg-brand" />
          )}
        </>
      )}
    </NavLink>
  );
}
