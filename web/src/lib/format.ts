// Centralized human-facing formatting. Components must route timestamps,
// durations, sizes and numbers through here so locale + the user's clock
// preference are applied consistently. Internally everything is canonical
// (epoch ms, bytes); conversion happens only at this display boundary.

let clock24 = true;
let locale: string | undefined; // undefined = browser default

/** Set by the preferences layer; flips 24h vs 12h clock across the app. */
export function setClock24(v: boolean): void {
  clock24 = v;
}

/** Set by the i18n layer so dates/numbers format in the chosen language. */
export function setLocale(l: string | undefined): void {
  locale = l || undefined;
}

export function fmtTime(ms: number): string {
  return new Date(ms).toLocaleTimeString(locale, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: !clock24,
  });
}

export function fmtTimeSec(ms: number): string {
  return new Date(ms).toLocaleTimeString(locale, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: !clock24,
  });
}

export function fmtDate(ms: number): string {
  return new Date(ms).toLocaleDateString(locale, {
    year: "numeric",
    month: "short",
    day: "2-digit",
  });
}

export function fmtDateTime(ms: number): string {
  return new Date(ms).toLocaleString(locale, {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: !clock24,
  });
}

export function fmtRelative(ms: number): string {
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  const diff = ms - Date.now();
  const abs = Math.abs(diff);
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ["day", 86_400_000],
    ["hour", 3_600_000],
    ["minute", 60_000],
  ];
  for (const [unit, per] of units) {
    if (abs >= per) return rtf.format(Math.round(diff / per), unit);
  }
  return abs < 5000 ? "just now" : rtf.format(Math.round(diff / 1000), "second");
}

/** Compact duration like "1h 04m" / "3m 12s" / "8s". */
export function fmtDuration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (h > 0) return `${h}h ${String(m).padStart(2, "0")}m`;
  if (m > 0) return `${m}m ${String(sec).padStart(2, "0")}s`;
  return `${sec}s`;
}

export function formatBytes(n: number): string {
  if (!n || n < 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${units[i]}`;
}
