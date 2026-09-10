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

  // The exact-array assertion is the append-only contract: a slot may be added to
  // the END, never inserted or reordered, because host mount points are matched by
  // position in first-party UI and an installed extension's manifest names a slot
  // by string. Changing this list deliberately fails here first.
  it("exposes exactly the known slots, in order (append-only contract)", () => {
    expect(EXT_SLOTS).toEqual([
      "camera-detail",
      "settings",
      "dashboard-card",
      "nav-panel",
      "player-toolbar",
      "player-osd",
    ]);
  });

  // Every camera-context slot must be grant-gated. This previously read
  // `slot !== "camera-detail"`, so any new player slot would have rendered on every
  // camera regardless of the admin's grant — a silent widening of what an extension
  // can see, not just where it appears.
  it("gates every camera-context slot by camera grant, not just camera-detail", () => {
    const scoped: ExtPanel = { extId: "a.b", extName: "A", slot: "", entry: "u.html", cameraGrants: ["cam-1"] };
    for (const slot of ["camera-detail", "player-toolbar", "player-osd"]) {
      const p = { ...scoped, slot };
      expect(panelsForSlot([p], slot, "cam-1")).toHaveLength(1);
      expect(panelsForSlot([p], slot, "cam-2")).toHaveLength(0);
      expect(panelsForSlot([p], slot)).toHaveLength(0);
    }
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

describe("camera-detail — the grant gates panel presence", () => {
  const all: ExtPanel = { extId: "all", extName: "All", slot: "camera-detail", entry: "ui/a.html", cameraGrants: [] };
  const hallwayOnly: ExtPanel = { extId: "hall", extName: "Hall", slot: "camera-detail", entry: "ui/h.html", cameraGrants: ["hallway"] };
  const panels = [all, hallwayOnly];

  it("shows an empty-grant panel on every camera (all-request or pure UI)", () => {
    expect(panelsForSlot(panels, "camera-detail", "hallway")).toContain(all);
    expect(panelsForSlot(panels, "camera-detail", "garage")).toContain(all);
  });

  it("shows a specific-grant panel only on its granted cameras", () => {
    expect(panelsForSlot(panels, "camera-detail", "hallway")).toContain(hallwayOnly);
    expect(panelsForSlot(panels, "camera-detail", "garage")).not.toContain(hallwayOnly);
  });

  it("hides a specific-grant panel when there is no camera context", () => {
    expect(panelsForSlot(panels, "camera-detail")).toEqual([all]);
  });
});
