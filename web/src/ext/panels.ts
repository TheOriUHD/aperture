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
}

// The host slots Pass 1 exposes. Extensions may only APPEND to these — never
// reorder or replace first-party UI — so with zero extensions every surface is
// byte-identical to before.
export const EXT_SLOTS = ["camera-detail", "settings", "dashboard-card", "nav-panel"] as const;
export type ExtSlot = (typeof EXT_SLOTS)[number];

// panelsForSlot filters installed panels to one slot. With no panels it returns
// [] for every slot — which is what makes ExtensionSlot render nothing.
export function panelsForSlot(panels: ExtPanel[], slot: string): ExtPanel[] {
  return panels.filter((p) => p.slot === slot);
}
