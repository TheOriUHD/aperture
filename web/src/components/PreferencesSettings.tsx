import { SlidersHorizontal } from "lucide-react";
import { Switch } from "./ui/Switch";
import { usePrefs } from "../lib/prefs";
import type { Preferences } from "../lib/types";

const LANDINGS: { value: Preferences["landing"]; label: string }[] = [
  { value: "dashboard", label: "Live wall" },
  { value: "playback", label: "Playback" },
  { value: "alarms", label: "Alarms" },
  { value: "cameras", label: "Cameras" },
];

const ZOOM_PRESETS: { idx: number; label: string }[] = [
  { idx: 0, label: "12 hours" },
  { idx: 2, label: "4 hours" },
  { idx: 4, label: "2 hours" },
  { idx: 6, label: "1 hour" },
  { idx: 8, label: "30 minutes" },
  { idx: 11, label: "10 minutes" },
];

export function PreferencesSettings() {
  const { prefs, setPref } = usePrefs();

  return (
    <div className="panel">
      <h2 className="flex items-center gap-2 text-sm font-medium text-fg mb-3">
        <SlidersHorizontal className="h-4 w-4" />
        Preferences
      </h2>
      <p className="text-xs text-fg-faint mb-4 leading-relaxed">
        These apply to your account on every device.
      </p>

      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <span className="text-sm text-fg">24-hour clock</span>
            <p className="text-xs text-fg-faint">Show times as 14:30 rather than 2:30 PM</p>
          </div>
          <Switch
            checked={prefs.clock24}
            onChange={(v) => setPref({ clock24: v })}
            label="24-hour clock"
          />
        </div>

        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="label">Open to</label>
            <select
              className="input"
              value={prefs.landing}
              onChange={(e) => setPref({ landing: e.target.value as Preferences["landing"] })}
            >
              {LANDINGS.map((l) => (
                <option key={l.value} value={l.value}>
                  {l.label}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="label">Default timeline span</label>
            <select
              className="input"
              value={prefs.timelineZoom}
              onChange={(e) => setPref({ timelineZoom: Number(e.target.value) })}
            >
              {ZOOM_PRESETS.map((z) => (
                <option key={z.idx} value={z.idx}>
                  {z.label}
                </option>
              ))}
            </select>
          </div>
        </div>

        <div>
          <label className="label">Density</label>
          <div className="flex gap-2">
            {(["comfortable", "compact"] as const).map((d) => (
              <button
                key={d}
                onClick={() => setPref({ density: d })}
                className={`btn h-9 flex-1 capitalize ${
                  prefs.density === d
                    ? "bg-brand/15 text-brand border border-brand/40"
                    : "bg-ink-800 text-fg-muted border border-line-strong hover:bg-ink-700"
                }`}
              >
                {d}
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
