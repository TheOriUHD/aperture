import { useEffect, useState } from "react";
import { MapPin, Search, Sunrise, Sunset, Sparkles, Loader2 } from "lucide-react";
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
    <div className="panel">
      <div className="flex items-center justify-between mb-3">
        <h2 className="flex items-center gap-2 text-sm font-medium text-fg">
          <MapPin className="h-4 w-4" />
          Location &amp; weather
        </h2>
        {site?.configured && wxEnabled && status && (
          <span className={`pill ${status.connected ? "pill-online" : "bg-ink-700 text-fg-faint"}`}>
            <span className={`h-1.5 w-1.5 rounded-full ${status.connected ? "bg-online" : "bg-fg-faint"}`} />
            {status.connected
              ? `Updated ${fmtTime(status.lastUpdated * 1000)}`
              : status.lastError
                ? "Error"
                : "Connecting…"}
          </span>
        )}
      </div>

      <p className="text-xs text-fg-faint mb-4 leading-relaxed">
        Sets where this NVR lives, so playback can reconstruct the weather, sun, and moon at any past
        moment. Weather comes from Open-Meteo — free, no API key.
      </p>

      {/* Type-ahead search with suggestions */}
      <label className="label">Search address or place</label>
      <div className="relative">
        <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-fg-faint">
          {searching ? <Loader2 className="h-4 w-4 animate-spin" /> : <Search className="h-4 w-4" />}
        </span>
        <input
          className="input pl-9"
          placeholder="Start typing a town or address…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        {results.length > 0 && (
          <div className="absolute z-20 mt-1 w-full max-h-64 overflow-auto rounded-md border border-line bg-ink-850 p-1 shadow-pop">
            {results.map((r, i) => (
              <button
                key={i}
                className="w-full text-left rounded px-2.5 py-1.5 hover:bg-ink-700"
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

      {/* Map with draggable pin */}
      <div className="mt-3">
        <MapPicker lat={Number(lat) || 0} lon={Number(lon) || 0} onChange={onMapChange} />
        <p className="text-[11px] text-fg-faint mt-1.5">
          Drag the pin or click the map to fine-tune. {addressLabel && <span className="text-fg-muted">{addressLabel}</span>}
        </p>
      </div>

      {/* Resolved coordinates (editable for the offline path) */}
      <div className="grid grid-cols-3 gap-3 mt-3">
        <Field label="Latitude">
          <input className="input" value={lat} onChange={(e) => setLat(e.target.value)} />
        </Field>
        <Field label="Longitude">
          <input className="input" value={lon} onChange={(e) => setLon(e.target.value)} />
        </Field>
        <Field label="Elevation (m)">
          <input className="input" value={elevationM} onChange={(e) => setElevationM(e.target.value)} />
        </Field>
      </div>
      <div className="mt-3">
        <Field label="Timezone">
          <input
            className="input"
            value={timezone}
            onChange={(e) => setTimezone(e.target.value)}
            placeholder="Europe/Berlin"
          />
        </Field>
      </div>

      {/* Units (the 12/24-hour clock is the app-wide preference, set in Preferences) */}
      <div className="grid grid-cols-3 gap-3 mt-3">
        <Field label="Temperature">
          <UnitSelect value={units.temp} opts={TEMP_UNITS} onChange={(v) => setUnits({ ...units, temp: v })} />
        </Field>
        <Field label="Wind">
          <UnitSelect value={units.wind} opts={WIND_UNITS} onChange={(v) => setUnits({ ...units, wind: v })} />
        </Field>
        <Field label="Pressure">
          <UnitSelect value={units.pressure} opts={PRESSURE_UNITS} onChange={(v) => setUnits({ ...units, pressure: v })} />
        </Field>
      </div>

      {/* Weather polling */}
      <div className="flex items-center justify-between mt-4">
        <div>
          <span className="text-sm text-fg">Record weather history</span>
          <p className="text-xs text-fg-faint">Polls conditions into a time-series for playback</p>
        </div>
        <Switch checked={wxEnabled} onChange={setWxEnabled} label="Record weather" />
      </div>
      {wxEnabled && (
        <Field label="Poll interval" className="mt-3 max-w-[12rem]">
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
        </Field>
      )}

      <div className="flex items-center gap-3 mt-4">
        <button className="btn-primary" disabled={!canSave || setSite.isPending} onClick={save}>
          {setSite.isPending ? "Saving…" : "Save"}
        </button>
        {saved && <span className="text-xs text-online">Saved.</span>}
        {setSite.isError && <span className="text-xs text-rec">Couldn’t save — check coordinates.</span>}
      </div>

      {/* Live preview: current conditions + today's sun/moon */}
      {site?.configured && (cur || astro?.configured) && (
        <div className="border-t border-line mt-5 pt-4 grid grid-cols-2 gap-4">
          {cur && (
            <div className="flex items-center gap-3">
              <CurIcon className="h-8 w-8 text-fg-muted shrink-0" />
              <div className="min-w-0">
                <p className="text-lg font-medium text-fg leading-tight">{formatTemp(cur.tempC, units)}</p>
                <p className="text-xs text-fg-muted truncate">{wmoText(cur.conditionCode)}</p>
                <p className="text-xs text-fg-faint truncate">
                  {cur.humidityPct != null && <>{Math.round(cur.humidityPct)}% RH · </>}
                  {formatWind(cur.windSpeedMs, units)} {compass(cur.windDirDeg)}
                </p>
              </div>
            </div>
          )}
          {astro?.configured && astro.sun && (
            <div className="text-xs text-fg-muted space-y-1">
              <div className="flex items-center gap-2">
                <Sunrise className="h-3.5 w-3.5 text-fg-faint" />
                <span>Sunrise {fmtTime(astro.sun.sunrise * 1000, timezone)}</span>
              </div>
              <div className="flex items-center gap-2">
                <Sunset className="h-3.5 w-3.5 text-fg-faint" />
                <span>Sunset {fmtTime(astro.sun.sunset * 1000, timezone)}</span>
              </div>
              {astro.moon && (
                <div className="flex items-center gap-2">
                  <Sparkles className="h-3.5 w-3.5 text-fg-faint" />
                  <span className="capitalize">
                    {astro.moon.name} · {Math.round(astro.moon.fraction * 100)}% lit
                  </span>
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
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
    <select className="input" value={value} onChange={(e) => onChange(e.target.value)}>
      {opts.map(([v, label]) => (
        <option key={v} value={v}>
          {label}
        </option>
      ))}
    </select>
  );
}
