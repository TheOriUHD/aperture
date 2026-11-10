import { useEffect, useState } from "react";
import { Search, Sunrise, Sunset, Sparkles, Loader2 } from "lucide-react";
import { SettingsHeading, SettingsRow } from "./ui/Layout";
import { Switch } from "./ui/Switch";
import { MapPicker } from "./MapPicker";
import {
  useSite,
  useSetSite,
  useWeather,
  useSetWeather,
  useAstro,
  geocode,
  resolveGeo,
  type GeoResult,
} from "../hooks/useEnvironment";
import {
  wmoIcon,
  wmoText,
  formatTemp,
  formatWind,
  compass,
  defaultUnits,
  type Units,
} from "../lib/weather";
import { fmtTime } from "../lib/format";

const TEMP_UNITS = [
  ["c", "Celsius (°C)"],
  ["f", "Fahrenheit (°F)"],
];
const WIND_UNITS = [
  ["ms", "m/s"],
  ["kmh", "km/h"],
  ["mph", "mph"],
  ["kn", "knots"],
];
const PRESSURE_UNITS = [
  ["hpa", "hPa"],
  ["inhg", "inHg"],
];

export function LocationWeather() {
  const { data: site } = useSite();
  const { data: wx } = useWeather();
  const setSite = useSetSite();
  const setWeather = useSetWeather();
  const { data: astro } = useAstro();

  const [query, setQuery] = useState("");
  const [results, setResults] = useState<GeoResult[]>([]);
  const [searching, setSearching] = useState(false);

  const [lat, setLat] = useState("");
  const [lon, setLon] = useState("");
  const [elevationM, setElevationM] = useState("");
  const [timezone, setTimezone] = useState("");
  const [addressLabel, setAddressLabel] = useState("");
  const [units, setUnits] = useState<Units>(defaultUnits);

  const [wxEnabled, setWxEnabled] = useState(true);
  const [intervalMin, setIntervalMin] = useState(10);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    if (!site) return;
    if (site.configured) {
      setLat(String(site.lat));
      setLon(String(site.lon));
      setElevationM(String(site.elevationM ?? 0));
      setTimezone(site.timezone);
      setAddressLabel(site.addressLabel);
    }
    setUnits({ ...defaultUnits, ...site.units });
  }, [site]);

  useEffect(() => {
    if (!wx) return;
    setWxEnabled(wx.enabled);
    setIntervalMin(wx.intervalMin || 10);
  }, [wx]);

  // Type-ahead: debounced geocode as the operator types.
  useEffect(() => {
    if (query.trim().length < 2) {
      setResults([]);
      return;
    }
    const h = setTimeout(async () => {
      setSearching(true);
      try {
        const r = await geocode(query.trim());
        setResults(r.results ?? []);
      } catch {
        setResults([]);
      } finally {
        setSearching(false);
      }
    }, 300);
    return () => clearTimeout(h);
  }, [query]);

  function pick(r: GeoResult) {
    setLat(String(r.lat));
    setLon(String(r.lon));
    setElevationM(String(Math.round(r.elevationM)));
    setTimezone(r.timezone);
    setAddressLabel(r.label);
    setResults([]);
    setQuery("");
    setSaved(false);
  }

  // Map pin drag/click: update coordinates, then resolve timezone + elevation.
  async function onMapChange(newLat: number, newLon: number) {
    setLat(String(newLat));
    setLon(String(newLon));
    setSaved(false);
    try {
      const r = await resolveGeo(newLat, newLon);
      if (r.timezone) setTimezone(r.timezone);
      if (typeof r.elevationM === "number") setElevationM(String(Math.round(r.elevationM)));
    } catch {
      /* keep existing tz/elevation */
    }
  }

  async function save() {
    await setSite.mutateAsync({
      lat: Number(lat),
      lon: Number(lon),
      elevationM: Number(elevationM) || 0,
      timezone,
      addressLabel,
      units,
    });
    await setWeather.mutateAsync({ enabled: wxEnabled, source: "open-meteo", intervalMin });
    setSaved(true);
    setTimeout(() => setSaved(false), 1500);
  }

  const canSave = lat !== "" && lon !== "" && !Number.isNaN(Number(lat)) && !Number.isNaN(Number(lon));
  const cur = wx?.current ?? null;
  const status = wx?.status;
  const CurIcon = wmoIcon(cur?.conditionCode, true);

  return (
    <>
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <div className="min-w-0 max-w-2xl">
          <h3 className="text-sm font-semibold text-fg">Location &amp; weather</h3>
          <p className="mt-1 text-xs leading-relaxed text-fg-faint">
            Sets where this NVR lives, so playback can reconstruct the weather, sun, and moon at any past
            moment. Weather comes from Open-Meteo — free, no API key.
          </p>
        </div>
        {site?.configured && wxEnabled && status && (
          <span className={status.connected ? "pill-green" : status.lastError ? "pill-red" : "pill-gray"}>
            <span
              className={`h-1.5 w-1.5 rounded-full ${
                status.connected ? "bg-online" : status.lastError ? "bg-danger" : "bg-fg-faint"
              }`}
            />
            {status.connected
              ? `Updated ${fmtTime(status.lastUpdated * 1000)}`
              : status.lastError
                ? "Error"
                : "Connecting…"}
          </span>
        )}
      </div>

      {/* Live preview: current conditions + today's sun/moon */}
      {site?.configured && (cur || astro?.configured) && (
        <div className="panel flex flex-col gap-5 sm:flex-row sm:items-center sm:gap-10">
          {cur && (
            <div className="flex min-w-0 items-center gap-3.5">
              <CurIcon className="h-9 w-9 shrink-0 text-fg-muted" strokeWidth={1.5} />
              <div className="min-w-0">
                <p className="text-xl font-semibold leading-tight text-fg tabular-nums">{formatTemp(cur.tempC, units)}</p>
                <p className="truncate text-xs text-fg-muted">{wmoText(cur.conditionCode)}</p>
              </div>
              <p className="ml-2 truncate border-l border-line pl-4 text-xs text-fg-faint">
                {cur.humidityPct != null && <>{Math.round(cur.humidityPct)}% RH · </>}
                {formatWind(cur.windSpeedMs, units)} {compass(cur.windDirDeg)}
              </p>
            </div>
          )}
          {astro?.configured && astro.sun && (
            <div className="flex flex-wrap items-center gap-x-5 gap-y-1.5 text-xs text-fg-muted sm:ml-auto">
              <span className="flex items-center gap-1.5">
                <Sunrise className="h-3.5 w-3.5 text-fg-faint" />
                Sunrise {fmtTime(astro.sun.sunrise * 1000, timezone)}
              </span>
              <span className="flex items-center gap-1.5">
                <Sunset className="h-3.5 w-3.5 text-fg-faint" />
                Sunset {fmtTime(astro.sun.sunset * 1000, timezone)}
              </span>
              {astro.moon && (
                <span className="flex items-center gap-1.5 capitalize">
                  <Sparkles className="h-3.5 w-3.5 text-fg-faint" />
                  {astro.moon.name} · {Math.round(astro.moon.fraction * 100)}% lit
                </span>
              )}
            </div>
          )}
        </div>
      )}

      <SettingsHeading>Location</SettingsHeading>
      <div className="divide-y divide-line">
        {/* Type-ahead search with suggestions */}
        <SettingsRow label="Search address or place">
          <div className="relative max-w-lg">
            <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-fg-faint">
              {searching ? <Loader2 className="h-4 w-4 animate-spin" /> : <Search className="h-4 w-4" />}
            </span>
            <input
              className="input pl-9"
              placeholder="Start typing a town or address…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              aria-label="Search address or place"
            />
            {results.length > 0 && (
              <div className="card absolute z-20 mt-1 max-h-64 w-full overflow-auto p-1 shadow-pop">
                {results.map((r, i) => (
                  <button
                    key={i}
                    className="w-full rounded-md px-2.5 py-1.5 text-left hover:bg-ink-700"
                    onClick={() => pick(r)}
                  >
                    <span className="text-sm text-fg">{r.label}</span>
                    <span className="block text-[11px] text-fg-faint">
                      {r.lat.toFixed(3)}, {r.lon.toFixed(3)} · {r.timezone}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </div>
        </SettingsRow>

        {/* Map with draggable pin */}
        <SettingsRow
          label="Map"
          align="start"
          hint={
            <>
              Drag the pin or click the map to fine-tune.
              {addressLabel && <span className="mt-1 block text-fg-muted">{addressLabel}</span>}
            </>
          }
        >
          <div className="max-w-2xl">
            <MapPicker lat={Number(lat) || 0} lon={Number(lon) || 0} onChange={onMapChange} />
          </div>
        </SettingsRow>

        {/* Resolved coordinates (editable for the offline path) */}
        <SettingsRow label="Coordinates" align="start">
          <div className="grid max-w-2xl grid-cols-3 gap-3">
            <Field label="Latitude">
              <input className="input tabular-nums" value={lat} onChange={(e) => setLat(e.target.value)} />
            </Field>
            <Field label="Longitude">
              <input className="input tabular-nums" value={lon} onChange={(e) => setLon(e.target.value)} />
            </Field>
            <Field label="Elevation (m)">
              <input className="input tabular-nums" value={elevationM} onChange={(e) => setElevationM(e.target.value)} />
            </Field>
          </div>
        </SettingsRow>
        <SettingsRow label="Timezone">
          <input
            className="input max-w-[280px]"
            value={timezone}
            onChange={(e) => setTimezone(e.target.value)}
            placeholder="Europe/Berlin"
            aria-label="Timezone"
          />
        </SettingsRow>
      </div>

      {/* Units (the 12/24-hour clock is the app-wide preference, set in Preferences) */}
      <SettingsHeading>Units</SettingsHeading>
      <div className="divide-y divide-line">
        <SettingsRow label="Temperature">
          <UnitSelect value={units.temp} opts={TEMP_UNITS} onChange={(v) => setUnits({ ...units, temp: v })} />
        </SettingsRow>
        <SettingsRow label="Wind">
          <UnitSelect value={units.wind} opts={WIND_UNITS} onChange={(v) => setUnits({ ...units, wind: v })} />
        </SettingsRow>
        <SettingsRow label="Pressure">
          <UnitSelect value={units.pressure} opts={PRESSURE_UNITS} onChange={(v) => setUnits({ ...units, pressure: v })} />
        </SettingsRow>
      </div>

      {/* Weather polling */}
      <SettingsHeading>Weather</SettingsHeading>
      <div className="divide-y divide-line">
        <SettingsRow label="Record weather history" hint="Polls conditions into a time-series for playback">
          <Switch checked={wxEnabled} onChange={setWxEnabled} label="Record weather" />
        </SettingsRow>
        {wxEnabled && (
          <SettingsRow label="Poll interval">
            <UnitSelect
              value={String(intervalMin)}
              opts={[
                ["5", "Every 5 min"],
                ["10", "Every 10 min"],
                ["15", "Every 15 min"],
                ["30", "Every 30 min"],
                ["60", "Hourly"],
              ]}
              onChange={(v) => setIntervalMin(Number(v))}
            />
          </SettingsRow>
        )}
        <SettingsRow label="">
          <div className="flex flex-wrap items-center gap-3">
            <button className="btn-primary" disabled={!canSave || setSite.isPending} onClick={save}>
              {setSite.isPending ? "Saving…" : "Save"}
            </button>
            {saved && <span className="text-xs text-online">Saved.</span>}
            {setSite.isError && <span className="text-xs text-danger">Couldn’t save — check coordinates.</span>}
          </div>
        </SettingsRow>
      </div>
    </>
  );
}

function Field({
  label,
  children,
  className,
}: {
  label: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={className}>
      <label className="label">{label}</label>
      {children}
    </div>
  );
}

function UnitSelect({
  value,
  opts,
  onChange,
}: {
  value: string;
  opts: string[][];
  onChange: (v: string) => void;
}) {
  return (
    <select className="input max-w-[240px]" value={value} onChange={(e) => onChange(e.target.value)}>
      {opts.map(([v, label]) => (
        <option key={v} value={v}>
          {label}
        </option>
      ))}
    </select>
  );
}
