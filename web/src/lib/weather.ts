// Shared weather/units formatting for the context layer — used by the Settings
// panel, the live ConditionsOverlay, and playback. Source values are always
// metric (°C, m/s, hPa); we convert at the edge per the site's unit choices.
import {
  Sun,
  Moon,
  CloudSun,
  Cloud,
  Cloudy,
  CloudFog,
  CloudDrizzle,
  CloudRain,
  CloudSnow,
  CloudLightning,
  type LucideIcon,
} from "lucide-react";

export interface Units {
  temp: string; // "c" | "f"
  wind: string; // "ms" | "kmh" | "mph" | "kn"
  pressure: string; // "hpa" | "inhg"
}

export const defaultUnits: Units = { temp: "c", wind: "ms", pressure: "hpa" };

// wmoText maps a WMO weather code to a short human label.
export function wmoText(code?: number | null): string {
  if (code == null) return "—";
  if (code === 0) return "Clear";
  if (code === 1) return "Mainly clear";
  if (code === 2) return "Partly cloudy";
  if (code === 3) return "Overcast";
  if (code === 45 || code === 48) return "Fog";
  if (code >= 51 && code <= 57) return "Drizzle";
  if (code >= 61 && code <= 67) return "Rain";
  if (code >= 71 && code <= 77) return "Snow";
  if (code >= 80 && code <= 82) return "Rain showers";
  if (code === 85 || code === 86) return "Snow showers";
  if (code >= 95) return "Thunderstorm";
  return "—";
}

// wmoIcon maps a WMO code (and day/night) to a lucide icon.
export function wmoIcon(code?: number | null, isDay = true): LucideIcon {
  if (code == null) return Cloud;
  if (code === 0 || code === 1) return isDay ? Sun : Moon;
  if (code === 2) return CloudSun;
  if (code === 3) return Cloudy;
  if (code === 45 || code === 48) return CloudFog;
  if (code >= 51 && code <= 57) return CloudDrizzle;
  if ((code >= 61 && code <= 67) || (code >= 80 && code <= 82)) return CloudRain;
  if ((code >= 71 && code <= 77) || code === 85 || code === 86) return CloudSnow;
  if (code >= 95) return CloudLightning;
  return Cloud;
}

export function formatTemp(c?: number | null, units: Units = defaultUnits): string {
  if (c == null) return "—";
  const v = units.temp === "f" ? c * 1.8 + 32 : c;
  return `${Math.round(v)}°${units.temp === "f" ? "F" : ""}`;
}

export function formatWind(ms?: number | null, units: Units = defaultUnits): string {
  if (ms == null) return "—";
  switch (units.wind) {
    case "kmh":
      return `${Math.round(ms * 3.6)} km/h`;
    case "mph":
      return `${Math.round(ms * 2.23694)} mph`;
    case "kn":
      return `${Math.round(ms * 1.94384)} kn`;
    default:
      return `${ms.toFixed(1)} m/s`;
  }
}

export function formatPressure(hpa?: number | null, units: Units = defaultUnits): string {
  if (hpa == null) return "—";
  if (units.pressure === "inhg") return `${(hpa * 0.02953).toFixed(2)} inHg`;
  return `${Math.round(hpa)} hPa`;
}

// compass turns a wind bearing (degrees, "from") into a 16-point label.
export function compass(deg?: number | null): string {
  if (deg == null) return "";
  const dirs = [
    "N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
    "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW",
  ];
  return dirs[Math.round(deg / 22.5) % 16];
}
