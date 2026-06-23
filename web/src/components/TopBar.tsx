import { useState, type ReactNode } from "react";
import { ChevronDown, LogOut, HardDrive } from "lucide-react";
import { useAuth } from "../lib/auth";

export function TopBar({ title, right }: { title: ReactNode; right?: ReactNode }) {
  const { user, logout } = useAuth();
  const [menu, setMenu] = useState(false);

  const initials = (user?.name || user?.email || "?")
    .split(/[\s@.]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((s) => s[0]?.toUpperCase())
    .join("");

  return (
    <header className="flex items-center gap-3 h-14 px-4 border-b border-line bg-ink-900/80 backdrop-blur shrink-0">
      <div className="flex items-center gap-2">
        <span className="text-sm font-medium text-fg">Home NVR</span>
        <ChevronDown className="h-4 w-4 text-fg-faint" />
      </div>
      <div className="w-px h-5 bg-line mx-1" />
      <div className="text-sm text-fg-muted">{title}</div>

      <div className="ml-auto flex items-center gap-2">
        {right}
        <div className="hidden sm:flex items-center gap-1.5 text-xs text-fg-faint px-2">
          <HardDrive className="h-3.5 w-3.5" />
          <span>1.4 / 4.0 TB</span>
        </div>
        <div className="relative">
          <button
            onClick={() => setMenu((m) => !m)}
            className="h-7 w-7 rounded-full bg-ink-700 text-fg-muted text-[11px] font-medium grid place-items-center hover:bg-ink-650"
            aria-label="Account menu"
          >
            {initials}
          </button>
          {menu && (
            <>
              <div className="fixed inset-0 z-10" onClick={() => setMenu(false)} />
              <div className="absolute right-0 mt-2 w-52 card shadow-pop z-20 py-1 animate-fade-in">
                <div className="px-3 py-2 border-b border-line">
                  <p className="text-sm text-fg truncate">{user?.name || user?.email}</p>
                  <p className="text-xs text-fg-faint capitalize">{user?.role}</p>
                </div>
                <button
                  onClick={() => void logout()}
                  className="w-full flex items-center gap-2 px-3 py-2 text-sm text-fg-muted hover:bg-ink-700 hover:text-fg"
                >
                  <LogOut className="h-4 w-4" />
                  Sign out
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </header>
  );
}
