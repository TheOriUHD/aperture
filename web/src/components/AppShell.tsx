import type { ReactNode } from "react";
import { NavRail } from "./NavRail";

export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-full bg-ink-900">
      <NavRail />
      <main className="flex-1 min-w-0 flex flex-col overflow-hidden">{children}</main>
    </div>
  );
}
