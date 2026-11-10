// Desktop dashboard helpers: which cameras a saved view shows, and how the camera
// wall lays them out. Pure functions, so the rules are unit-tested without a DOM.

/** The view id stored for a hand-picked set of cameras. */
export const CUSTOM_VIEW = "custom";

/** Parses the stored custom selection. Anything that is not a list of ids reads
 * as an empty set, which shows all cameras. Repeated ids keep their first place. */
export function parseCameraIds(raw: string | null | undefined): string[] {
  if (!raw) return [];
  try {
    const v: unknown = JSON.parse(raw);
    if (!Array.isArray(v)) return [];
    return [...new Set(v.filter((x): x is string => typeof x === "string" && x !== ""))];
  } catch {
    return [];
  }
}

interface Group {
  id: string;
  cameraIds: string[];
}

/** Resolves the stored view id to the cameras it shows.
 *
 * - A group shows its cameras in wall order.
 * - The custom view shows its cameras in the order they were picked. Ids that no
 *   longer match a camera on the wall (deleted or switched off) are skipped.
 * - A missing group, or a custom set with nothing left in it, falls back to all
 *   cameras, and `id` says which view is really showing. */
export function resolveView<C extends { id: string }>(
  cameras: C[],
  groups: Group[],
  viewId: string,
  customIds: string[],
): { id: string; shown: C[]; custom: C[] } {
  const byId = new Map(cameras.map((c) => [c.id, c]));
  const custom = customIds.map((id) => byId.get(id)).filter((c): c is C => !!c);
  if (viewId === CUSTOM_VIEW && custom.length > 0) return { id: CUSTOM_VIEW, shown: custom, custom };
  const group = viewId !== "all" && viewId !== CUSTOM_VIEW ? groups.find((g) => g.id === viewId) : undefined;
  if (group) return { id: group.id, shown: cameras.filter((c) => group.cameraIds.includes(c.id)), custom };
  return { id: "all", shown: cameras, custom };
}

/** Narrowest useful tile; below this the wall scrolls instead of shrinking. */
export const MIN_TILE_W = 260;

/** Lays `n` 16:9 tiles into a `w`×`h` box: the column count that gives the
 * biggest tiles (five cameras on a wide screen fill it better as 2+2+1 than as
 * 3+2). When even the best fit is narrower than MIN_TILE_W, the wall scrolls,
 * with as many MIN_TILE_W-or-wider columns as fit across. */
export function fitWall(n: number, w: number, h: number): { cols: number; tileW: number; tileH: number; scroll: boolean } {
  let cols = 1;
  let tileW = 0;
  for (let c = 1; c <= Math.max(1, n); c++) {
    const tw = Math.min(w / c, (h / Math.ceil(n / c)) * (16 / 9));
    if (tw > tileW + 0.5) {
      tileW = tw;
      cols = c;
    }
  }
  const scroll = tileW < MIN_TILE_W && w > 0;
  if (scroll) {
    cols = Math.max(1, Math.floor(w / MIN_TILE_W));
    tileW = Math.max(MIN_TILE_W, w / cols);
  }
  return { cols, tileW, tileH: (tileW * 9) / 16, scroll };
}
