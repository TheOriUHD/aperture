import { NavLink } from "react-router-dom";
import clsx from "clsx";
import {
  LayoutDashboard,
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
  label: string;
  icon: LucideIcon;
}

const items: NavItem[] = [
  { to: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
  { to: "/live", label: "Live view", icon: LayoutGrid },
  { to: "/cameras", label: "Cameras", icon: Cctv },
  { to: "/recordings", label: "Recordings", icon: Clapperboard },
  { to: "/alarms", label: "Alarms", icon: Bell },
];

export function NavRail() {
  return (
    <nav className="flex flex-col items-center gap-1.5 w-[54px] shrink-0 bg-ink-850 border-r border-line py-3">
      <div className="h-8 w-8 rounded-lg bg-brand grid place-items-center mb-3">
        <Aperture className="h-5 w-5 text-white" />
      </div>
      {items.map((item) => (
        <RailLink key={item.to} {...item} />
      ))}
      <div className="mt-auto">
        <RailLink to="/settings" label="Settings" icon={Settings} />
      </div>
    </nav>
  );
}

function RailLink({ to, label, icon: Icon }: NavItem) {
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
