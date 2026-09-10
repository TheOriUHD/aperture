// On-screen display (OSD) surfaces: the translucent widgets that float over a
// video — the name/time pill, the conditions badge, the zoom navigator, and
// anything an extension contributes through the `player-osd` slot.
//
// Their opacity is a single user preference rather than a per-widget decision.
// Before this existed, overlays hardcoded their own alpha and the codebase had
// accumulated nine different blacks (bg-black/70, /65, /60, /55, /50, /45, /40,
// /35, /30) layered over the same picture. An extension author choosing a tenth
// would not have been doing anything wrong — there was nothing to conform to.
//
// The value is published as a CSS custom property so that plain CSS can consume
// it, including inside an extension's sandboxed iframe, where React context and
// Tailwind classes cannot reach.

export const OSD_MIN_OPACITY = 0.15;
export const OSD_MAX_OPACITY = 1;

/** Name of the CSS custom property carrying the current OSD opacity. */
export const OSD_ALPHA_VAR = "--osd-alpha";

export function clampOsdOpacity(v: number): number {
  if (!Number.isFinite(v)) return 0.55;
  return Math.min(OSD_MAX_OPACITY, Math.max(OSD_MIN_OPACITY, v));
}

/**
 * Publish the OSD opacity to the document so every surface picks it up.
 *
 * Set on documentElement rather than on a React-rendered wrapper: overlays are
 * scattered across screens, portals and (for extensions) iframes, and a variable
 * on the root is the one place all of them can inherit from.
 */
export function applyOsdOpacity(v: number): void {
  if (typeof document === "undefined") return;
  document.documentElement.style.setProperty(OSD_ALPHA_VAR, String(clampOsdOpacity(v)));
}

/**
 * The inline style for an OSD surface, for callers that cannot use the
 * `.osd-surface` utility class — notably anything computing a background in JS,
 * and the host when it hands a value across the iframe boundary.
 */
export function osdSurfaceStyle(): Record<string, string> {
  return { background: `rgba(0, 0, 0, var(${OSD_ALPHA_VAR}, 0.55))` };
}
