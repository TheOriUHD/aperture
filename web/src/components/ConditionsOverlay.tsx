// Composited weather/condition readouts over live and playback video. These
// never touch the pixels — they're DOM layers above the shared <video>, so they
// cost nothing and respect the stream-copy ethos. Everything hides cleanly when
// no site location is configured.
import {
  useSite,
  useWeatherNow,
  useWeatherAt,
  useAstro,
  type WeatherSample,
  type SunTimes,
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

type Variant = "compact" | "full";

function isNightAt(tsSec: number, sun?: SunTimes): boolean {
  if (!sun || !sun.sunrise || !sun.sunset) return false;
  return tsSec < sun.sunrise || tsSec > sun.sunset;
}

// ConditionsBadge is the inline visual (no positioning, no fetching).
function ConditionsBadge({
  sample,
  units,
  variant,
  night,
}: {
  sample: WeatherSample;
  units: Units;
  variant: Variant;
  night: boolean;
}) {
  if (sample.tempC == null) return null;
  const Icon = wmoIcon(sample.conditionCode, !night);

  if (variant === "compact") {
    return (
      <div
        className="pointer-events-none flex items-center gap-1.5 rounded-md bg-black/40 px-2 py-1 backdrop-blur-sm"
        style={{ textShadow: "0 1px 2px rgba(0,0,0,0.8)" }}
      >
        <Icon className="h-3.5 w-3.5 text-white/90" />
        <span className="text-[12px] font-medium text-white tabular-nums">
          {formatTemp(sample.tempC, units)}
        </span>
      </div>
    );
  }

  return (
    <div className="pointer-events-none flex items-center gap-2 rounded-md bg-black/40 px-2.5 py-1 backdrop-blur-sm">
      <Icon className="h-4 w-4 text-white/90" />
      <span className="text-sm font-medium text-white tabular-nums">{formatTemp(sample.tempC, units)}</span>
      <span className="text-xs text-white/70">{wmoText(sample.conditionCode)}</span>
      {sample.windSpeedMs != null && (
        <span className="hidden sm:inline text-xs text-white/55 tabular-nums">
          {formatWind(sample.windSpeedMs, units)} {compass(sample.windDirDeg)}
        </span>
      )}
    </div>
  );
}

// LiveConditions shows current conditions over a live tile or focused viewer.
export function LiveConditions({
  variant = "full",
  className = "top-3 right-3",
}: {
  variant?: Variant;
  className?: string;
}) {
  const { data: site } = useSite();
  const { data: now } = useWeatherNow(!!site?.configured);
  const { data: astro } = useAstro();

  if (!site?.configured || !now?.sample) return null;
  const nowSec = Math.floor(Date.now() / 1000);
  return (
    <div className={`absolute z-10 ${className}`}>
      <ConditionsBadge
        sample={now.sample}
        units={site.units ?? defaultUnits}
        variant={variant}
        night={isNightAt(nowSec, astro?.sun)}
      />
    </div>
  );
}

// PlaybackConditions shows the conditions current at the playhead instant.
export function PlaybackConditions({
  tsMs,
  className = "top-3 right-3",
}: {
  tsMs: number;
  className?: string;
}) {
  const { data: site } = useSite();
  const { data: sample } = useWeatherAt(tsMs, !!site?.configured);
  const dateKey = new Date(tsMs).toLocaleDateString("en-CA", { timeZone: site?.timezone || undefined });
  const { data: astro } = useAstro(site?.configured ? dateKey : undefined);

  if (!site?.configured || !sample) return null;
  return (
    <div className={`absolute z-10 ${className}`}>
      <ConditionsBadge
        sample={sample}
        units={site.units ?? defaultUnits}
        variant="full"
        night={isNightAt(Math.floor(tsMs / 1000), astro?.sun)}
      />
    </div>
  );
}
