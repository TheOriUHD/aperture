// Hooks for the environmental context layer: site profile, weather (config,
// status, current, historical-at-playhead), and astronomy.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import type { Units } from "../lib/weather";

export interface Site {
  configured: boolean;
  lat: number;
  lon: number;
  elevationM: number;
  timezone: string;
  addressLabel: string;
  units: Units;
}

export interface GeoResult {
  label: string;
  name: string;
  country: string;
  admin1: string;
  lat: number;
  lon: number;
  elevationM: number;
  timezone: string;
}

export interface WeatherSample {
  ts: number;
  tempC?: number;
  feelsLikeC?: number;
  humidityPct?: number;
  dewpointC?: number;
  pressureHpa?: number;
  windSpeedMs?: number;
  windGustMs?: number;
  windDirDeg?: number;
  precipMm?: number;
  cloudPct?: number;
  conditionCode?: number;
  source?: string;
}

export interface WeatherStatus {
  configured: boolean;
  enabled: boolean;
  connected: boolean;
  lastUpdated: number;
  lastError?: string;
  source: string;
}

export interface WeatherConfig {
  enabled: boolean;
  source: string;
  intervalMin: number;
  status: WeatherStatus;
  current: WeatherSample | null;
}

export interface SunTimes {
  solarNoon: number;
  nadir: number;
  sunrise: number;
  sunset: number;
  sunriseEnd: number;
  sunsetStart: number;
  dawn: number;
  dusk: number;
  nauticalDawn: number;
  nauticalDusk: number;
  nightEnd: number;
  night: number;
  goldenHourEnd: number;
  goldenHour: number;
  alwaysUp: boolean;
  alwaysDown: boolean;
}

export interface MoonInfo {
  fraction: number;
  phase: number;
  angle: number;
  name: string;
}

export interface Astro {
  configured: boolean;
  date?: string;
  timezone?: string;
  sun?: SunTimes;
  moon?: MoonInfo;
}

export function useSite() {
  return useQuery({
    queryKey: ["site"],
    queryFn: () => api.get<Site>("/api/system/site"),
    staleTime: 60_000,
  });
}

export function useSetSite() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (s: Partial<Site>) => api.put<Site>("/api/system/site", s),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["site"] });
      qc.invalidateQueries({ queryKey: ["weather"] });
      qc.invalidateQueries({ queryKey: ["astro"] });
    },
  });
}

export function geocode(q: string) {
  return api.get<{ results: GeoResult[] }>(`/api/system/geocode?q=${encodeURIComponent(q)}`);
}

// resolveGeo returns timezone + elevation for dragged-pin coordinates.
export function resolveGeo(lat: number, lon: number) {
  return api.get<{ timezone: string; elevationM: number }>(
    `/api/system/geo/resolve?lat=${lat}&lon=${lon}`,
  );
}

export function useWeather() {
  return useQuery({
    queryKey: ["weather"],
    queryFn: () => api.get<WeatherConfig>("/api/system/weather"),
    refetchInterval: 15_000,
  });
}

export function useSetWeather() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { enabled: boolean; source: string; intervalMin: number }) =>
      api.put<WeatherConfig>("/api/system/weather", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["weather"] }),
  });
}

// useWeatherNow is the lightweight current-conditions feed for the live overlay
// (available to any authenticated user).
export function useWeatherNow(enabled = true) {
  return useQuery({
    queryKey: ["weather-now"],
    queryFn: () =>
      api.get<{
        sample: WeatherSample | null;
        connected: boolean;
        lastUpdated: number;
        source: string;
        configured: boolean;
      }>("/api/system/weather/now"),
    enabled,
    refetchInterval: 60_000,
  });
}

// useWeatherAt fetches the conditions current at a playhead instant. The query
// key buckets to 5-minute boundaries so scrubbing reuses cached samples instead
// of hammering the API; missing samples resolve to null rather than throwing.
export function useWeatherAt(tsMs?: number, enabled = true) {
  const bucket = tsMs ? Math.floor(tsMs / 1000 / 300) * 300 : 0;
  return useQuery({
    queryKey: ["weather-at", bucket],
    queryFn: async () => {
      try {
        return await api.get<WeatherSample>(`/api/system/weather/at?ts=${bucket}`);
      } catch {
        return null;
      }
    },
    enabled: enabled && bucket > 0,
    staleTime: Infinity,
  });
}

// useWeatherRange returns the samples within a window (ms in, seconds on the
// wire) for the timeline's environmental ribbon. Keyed by the day bounds so it
// fetches once per day rather than on every scrub.
export function useWeatherRange(fromMs: number, toMs: number, enabled = true) {
  const from = Math.floor(fromMs / 1000);
  const to = Math.floor(toMs / 1000);
  return useQuery({
    queryKey: ["weather-range", from, to],
    queryFn: () =>
      api
        .get<{ samples: WeatherSample[] }>(`/api/system/weather/range?from=${from}&to=${to}`)
        .then((r) => r.samples)
        .catch(() => [] as WeatherSample[]),
    enabled: enabled && to > from,
    staleTime: 5 * 60_000,
  });
}

export function useAstro(date?: string) {
  return useQuery({
    queryKey: ["astro", date ?? "today"],
    queryFn: () => api.get<Astro>(`/api/system/astro${date ? `?date=${date}` : ""}`),
    staleTime: 5 * 60_000,
  });
}
