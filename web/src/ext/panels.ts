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
  // The extension's camera grant. A specific grant gates a camera-context panel to
  // exactly those cameras; an empty grant (an "all" request, or a pure-UI panel that
  // requested no cameras) renders on every camera. This is the ONE decision that
  // controls both what a panel can read and where it appears.
  cameraGrants?: string[];
  // Corner a player-osd widget asks for ("top-left" … "bottom-right"); absent for
  // every other slot, and absent means bottom-left.
  anchor?: string;
  // The extension's icon, for slots that render a control rather than a card — a
  // player-toolbar entry is an icon button. A path under the extension's CSP'd
  // asset route, loaded as an <img>; never inlined markup.
  icon?: string;
}

// The host slots. Extensions may only APPEND to these — never reorder or replace
// first-party UI — so with zero extensions every surface is byte-identical to before.
//
// player-toolbar and player-osd are camera-context slots that live ON the video
// rather than under it:
//   player-toolbar — an icon in the player's top-right control cluster, beside the
//     picture-settings and camera-settings buttons, owning its own dropdown.
//   player-osd — a transparent widget floating over the picture, corner-anchored,
//     for things that want to sit on the image itself (a PTZ jog wheel, a readout).
// Both honour the user's single overlay-opacity preference; neither can style its
// way out of it. Because both are camera-context, both are gated by camera grant —
// see visibleForCamera.
export const EXT_SLOTS = [
  "camera-detail",
  "settings",
  "dashboard-card",
  "nav-panel",
  "player-toolbar",
  "player-osd",
] as const;
export type ExtSlot = (typeof EXT_SLOTS)[number];

// Slots that render in the context of one camera and must therefore respect the
// extension's camera grant. Kept as a set rather than a string compare so adding a
// camera-context slot cannot silently skip the grant check — the previous
// `slot !== "camera-detail"` test would have rendered a new player slot on every
// camera regardless of what the admin granted.
export const CAMERA_CONTEXT_SLOTS = new Set<string>([
  "camera-detail",
  "player-toolbar",
  "player-osd",
]);

// panelsForSlot filters installed panels to one slot, and — for the per-camera
// camera-detail slot — also to the camera on screen: a panel with a specific camera
// grant appears only on those cameras; an empty grant appears everywhere. With no
// panels it returns [] for every slot, which is what makes ExtensionSlot render
// nothing (the zero-extensions identity guarantee).
export function panelsForSlot(panels: ExtPanel[], slot: string, cameraId?: string): ExtPanel[] {
  return panels.filter((p) => p.slot === slot && visibleForCamera(p, slot, cameraId));
}

// visibleForCamera gates a camera-context panel by its camera grant. Slots that are
// not camera-context are never gated. An empty/absent grant = every camera (an "all"
// request or a pure-UI panel). A specific grant = only those cameras.
export function visibleForCamera(panel: ExtPanel, slot: string, cameraId?: string): boolean {
  if (!CAMERA_CONTEXT_SLOTS.has(slot)) return true;
  const grants = panel.cameraGrants ?? [];
  if (grants.length === 0) return true;
  return cameraId != null && grants.includes(cameraId);
}
