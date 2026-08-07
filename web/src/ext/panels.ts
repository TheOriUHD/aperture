// Extension UI panels (T1.6-web). A panel is one iframe an installed, enabled
// extension contributes to a named host slot. This module is deliberately pure
// (no React) so the "zero extensions ⇒ nothing renders" guarantee can be pinned
// by a vitest snapshot without a DOM.

export interface ExtPanel {
  extId: string;
  extName: string;
  slot: string;
  entry: string;
  title?: string;
  // The extension's camera grant. A specific grant gates a camera-detail panel to
  // exactly those cameras; an empty grant (an "all" request, or a pure-UI panel that
  // requested no cameras) renders on every camera. This is the ONE decision that
  // controls both what a panel can read and where it appears.
  cameraGrants?: string[];
}

// The host slots Pass 1 exposes. Extensions may only APPEND to these — never
// reorder or replace first-party UI — so with zero extensions every surface is
// byte-identical to before.
export const EXT_SLOTS = ["camera-detail", "settings", "dashboard-card", "nav-panel"] as const;
export type ExtSlot = (typeof EXT_SLOTS)[number];

// panelsForSlot filters installed panels to one slot, and — for the per-camera
// camera-detail slot — also to the camera on screen: a panel with a specific camera
// grant appears only on those cameras; an empty grant appears everywhere. With no
// panels it returns [] for every slot, which is what makes ExtensionSlot render
// nothing (the zero-extensions identity guarantee).
export function panelsForSlot(panels: ExtPanel[], slot: string, cameraId?: string): ExtPanel[] {
  return panels.filter((p) => p.slot === slot && visibleForCamera(p, slot, cameraId));
}

// visibleForCamera gates a camera-detail panel by its camera grant. Non-camera slots
// are never gated. An empty/absent grant = every camera (an "all" request or a
// pure-UI panel). A specific grant = only those cameras.
export function visibleForCamera(panel: ExtPanel, slot: string, cameraId?: string): boolean {
  if (slot !== "camera-detail") return true;
  const grants = panel.cameraGrants ?? [];
  if (grants.length === 0) return true;
  return cameraId != null && grants.includes(cameraId);
}
