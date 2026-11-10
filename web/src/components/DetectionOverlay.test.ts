import { describe, expect, it } from "vitest";
import { at, type TrackPoint } from "./DetectionOverlay";

const p = (ts: number, x: number, extra: Partial<TrackPoint> = {}): TrackPoint => ({
  eventId: "e",
  obj: 1,
  label: "person",
  confidence: 0.9,
  x,
  y: 0.2,
  w: 0.1,
  h: 0.5,
  ts,
  ...extra,
});

describe("object path interpolation", () => {
  const path = [p(1000, 0.1), p(1300, 0.16), p(1600, 0.22)];
  it("glides between known positions", () => {
    expect(at(path, 1150)?.x).toBeCloseTo(0.13, 5);
    expect(at(path, 1450)?.x).toBeCloseTo(0.19, 5);
  });
  it("holds briefly after the last position, then disappears", () => {
    expect(at(path, 1900)?.x).toBeCloseTo(0.22, 5);
    expect(at(path, 2400)).toBeNull();
    expect(at(path, 500)).toBeNull();
  });
  it("doesn't bridge long gaps", () => {
    const gappy = [p(1000, 0.1), p(9000, 0.9)];
    expect(at(gappy, 5000)).toBeNull();
    expect(at(gappy, 1200)?.x).toBeCloseTo(0.1, 5);
    expect(at(gappy, 9000)?.x).toBeCloseTo(0.9, 5);
  });
});
