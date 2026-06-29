// Day/night timeline shading: turn the astro layer's solar events into subtle
// background bands so the eye finds dusk/dawn at a glance while scrubbing.
import type { SunTimes } from "../hooks/useEnvironment";

export type ShadeKind = "night" | "twilight" | "golden";

// A shading band in unix milliseconds (matches the timeline's time domain).
export interface ShadeBand {
  start: number;
  end: number;
  kind: ShadeKind;
}

const S = 1000;

// buildShadeBands turns a few consecutive days' sun events into continuous
// bands: night (civil dusk → next civil dawn), twilight (the dawn/dusk wings),
// and golden hour. Daytime is left untinted. Endpoints of 0 (an event that does
// not occur, e.g. polar summer) are skipped so the band simply doesn't render.
export function buildShadeBands(days: (SunTimes | undefined)[]): ShadeBand[] {
  const suns = days.filter((d): d is SunTimes => !!d);
  const out: ShadeBand[] = [];
  const add = (a: number, b: number, kind: ShadeKind) => {
    if (a > 0 && b > 0 && b > a) out.push({ start: a * S, end: b * S, kind });
  };
  for (let i = 0; i < suns.length; i++) {
    const s = suns[i];
    const next = suns[i + 1];
    add(s.dawn, s.sunrise, "twilight");
    add(s.sunrise, s.goldenHourEnd, "golden");
    add(s.goldenHour, s.sunset, "golden");
    add(s.sunset, s.dusk, "twilight");
    if (next) add(s.dusk, next.dawn, "night");
  }
  return out;
}

export function shadeColor(kind: ShadeKind): string {
  switch (kind) {
    case "night":
      return "rgba(8,12,28,0.45)";
    case "twilight":
      return "rgba(70,80,140,0.20)";
    case "golden":
      return "rgba(232,168,74,0.15)";
  }
}
