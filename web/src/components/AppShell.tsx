import { createContext, useContext, useState, type ReactNode } from "react";
import { NavRail, BottomNav } from "./NavRail";
import { AppHeader } from "./TopBar";
import { DownloadsOverlay } from "./DownloadsOverlay";

// The header spans the full width above the rail, as in the console the app is
// styled after. Screens still declare their own title and header actions with
// <TopBar>, which renders into these slots through a portal, so no screen has to
// know where the header lives.
interface HeaderSlots {
  title: HTMLElement | null;
  right: HTMLElement | null;
}
const SlotCtx = createContext<HeaderSlots>({ title: null, right: null });
export const useHeaderSlots = () => useContext(SlotCtx);

export function AppShell({ children }: { children: ReactNode }) {
  const [title, setTitle] = useState<HTMLElement | null>(null);
  const [right, setRight] = useState<HTMLElement | null>(null);
  return (
    <SlotCtx.Provider value={{ title, right }}>
      <div className="flex h-full flex-col bg-ink-900">
        <AppHeader titleRef={setTitle} rightRef={setRight} />
        <div className="flex min-h-0 flex-1">
          <NavRail />
          <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
            <div className="flex min-h-0 flex-1 flex-col overflow-hidden">{children}</div>
            <BottomNav />
          </main>
        </div>
        <DownloadsOverlay />
      </div>
    </SlotCtx.Provider>
  );
}
