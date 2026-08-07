import { describe, expect, it } from "vitest";
import { EXT_SLOTS, panelsForSlot, type ExtPanel } from "./panels";

// The identity guarantee for T1.6, proven without a browser: every host slot
// (dashboard-card, settings, camera-detail, nav-panel) is fed by panelsForSlot,
// and ExtensionSlot renders nothing when it returns []. So if panelsForSlot is []
// for every slot when no extensions are installed, every first-party surface is
// byte-identical to before. The first-party arrays (NavRail items, Settings tabs,
// OverviewCards) are left untouched and only APPENDED to via <ExtensionSlot>, so
// there is no first-party registry to snapshot — identity holds by construction;
// this test pins the append-only data layer that backs it.

describe("panelsForSlot — the zero-extensions identity guarantee", () => {
  it("returns [] for every host slot when no extensions are installed", () => {
    for (const slot of EXT_SLOTS) {
      expect(panelsForSlot([], slot)).toEqual([]);
    }
  });

  it("exposes exactly the four Pass-1 slots, in order (append-only contract)", () => {
    expect(EXT_SLOTS).toEqual(["camera-detail", "settings", "dashboard-card", "nav-panel"]);
  });

  it("selects only the panels whose slot matches, preserving input order", () => {
    const panels: ExtPanel[] = [
      { extId: "a", extName: "A", slot: "dashboard-card", entry: "ui/a.html" },
      { extId: "b", extName: "B", slot: "settings", entry: "ui/b.html" },
      { extId: "c", extName: "C", slot: "dashboard-card", entry: "ui/c.html", title: "C card" },
    ];
    expect(panelsForSlot(panels, "dashboard-card")).toEqual([panels[0], panels[2]]);
    expect(panelsForSlot(panels, "settings")).toEqual([panels[1]]);
    expect(panelsForSlot(panels, "nav-panel")).toEqual([]);
  });

  it("does not mutate the input array", () => {
    const panels: ExtPanel[] = [
      { extId: "a", extName: "A", slot: "settings", entry: "ui/a.html" },
    ];
    const before = [...panels];
    panelsForSlot(panels, "settings");
    expect(panels).toEqual(before);
  });
});
