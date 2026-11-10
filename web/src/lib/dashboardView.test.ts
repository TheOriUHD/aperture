import { describe, expect, it } from "vitest";
import { CUSTOM_VIEW, MIN_TILE_W, fitWall, parseCameraIds, resolveView } from "./dashboardView";

const cams = ["a", "b", "c", "d"].map((id) => ({ id }));
const groups = [{ id: "g1", cameraIds: ["c", "a"] }];
const ids = (list: { id: string }[]) => list.map((c) => c.id);

describe("parseCameraIds", () => {
  it("reads a stored list and drops junk and repeats", () => {
    expect(parseCameraIds('["b","a",3,"",null,"b"]')).toEqual(["b", "a"]);
  });
  it("treats missing or malformed values as empty", () => {
    expect(parseCameraIds(null)).toEqual([]);
    expect(parseCameraIds("")).toEqual([]);
    expect(parseCameraIds("{")).toEqual([]);
    expect(parseCameraIds('{"a":1}')).toEqual([]);
  });
});

describe("resolveView", () => {
  it("shows all cameras by default", () => {
    const v = resolveView(cams, groups, "all", []);
    expect(v.id).toBe("all");
    expect(ids(v.shown)).toEqual(["a", "b", "c", "d"]);
  });
  it("shows a group's cameras in wall order", () => {
    const v = resolveView(cams, groups, "g1", []);
    expect(v.id).toBe("g1");
    expect(ids(v.shown)).toEqual(["a", "c"]);
  });
  it("falls back to all when the group is gone", () => {
    expect(resolveView(cams, groups, "deleted", []).id).toBe("all");
  });
  it("shows the custom set in the order it was picked, skipping unknown ids", () => {
    const v = resolveView(cams, groups, CUSTOM_VIEW, ["d", "gone", "b"]);
    expect(v.id).toBe(CUSTOM_VIEW);
    expect(ids(v.shown)).toEqual(["d", "b"]);
  });
  it("falls back to all when nothing is left in the custom set", () => {
    expect(resolveView(cams, groups, CUSTOM_VIEW, []).id).toBe("all");
    const v = resolveView(cams, groups, CUSTOM_VIEW, ["gone"]);
    expect(v.id).toBe("all");
    expect(v.custom).toEqual([]);
    expect(ids(v.shown)).toEqual(["a", "b", "c", "d"]);
  });
  it("reports the custom set while another view is showing", () => {
    const v = resolveView(cams, groups, "g1", ["b"]);
    expect(v.id).toBe("g1");
    expect(ids(v.custom)).toEqual(["b"]);
  });
});

describe("fitWall", () => {
  it("picks the column count with the biggest tiles", () => {
    // Five cameras: 2+2+1 on a 16:9 box, 3+2 on a wider one.
    expect(fitWall(5, 1600, 900).cols).toBe(2);
    expect(fitWall(5, 2000, 900).cols).toBe(3);
    // One camera on a portrait box spans the width.
    const one = fitWall(1, 700, 1800);
    expect(one.cols).toBe(1);
    expect(one.tileW).toBe(700);
    // Four cameras on a portrait box stack in one column.
    expect(fitWall(4, 700, 1800).cols).toBe(1);
  });
  it("scrolls when tiles would get too small", () => {
    const f = fitWall(40, 1200, 700);
    expect(f.scroll).toBe(true);
    expect(f.cols).toBe(Math.floor(1200 / MIN_TILE_W));
    expect(f.tileW).toBeGreaterThanOrEqual(MIN_TILE_W);
    expect(f.tileH).toBeCloseTo((f.tileW * 9) / 16);
  });
});
