import type { ReactNode } from "react";
import { NavRail, BottomNav } from "./NavRail";
import { DownloadsOverlay } from "./DownloadsOverlay";

export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-full bg-ink-900">
      <NavRail />
      <main className="flex-1 min-w-0 flex flex-col overflow-hidden">
        <div className="flex-1 min-h-0 flex flex-col overflow-hidden">{children}</div>
        <BottomNav />
      </main>
      <DownloadsOverlay />
    </div>
  );
}
