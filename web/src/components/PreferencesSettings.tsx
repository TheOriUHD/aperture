import { useTranslation } from "react-i18next";
import { Switch } from "./ui/Switch";
import { Segmented, SettingsHeading, SettingsRow } from "./ui/Layout";
import { usePrefs } from "../lib/prefs";
import type { Preferences } from "../lib/types";
import { OSD_MIN_OPACITY, OSD_MAX_OPACITY } from "../lib/osd";

const ZOOM_PRESETS: { idx: number; label: string }[] = [
  { idx: 0, label: "12 hours" },
  { idx: 2, label: "4 hours" },
  { idx: 4, label: "2 hours" },
  { idx: 6, label: "1 hour" },
  { idx: 8, label: "30 minutes" },
  { idx: 11, label: "10 minutes" },
];

// Rendered inside the General tab's flow (a heading and its rows, no wrapper), so
// it shares the page's rhythm with the Account and System groups around it.
export function PreferencesSettings() {
  const { prefs, setPref } = usePrefs();
  const { t } = useTranslation();

  const landings: { value: Preferences["landing"]; label: string }[] = [
    { value: "dashboard", label: t("prefs.liveWall", "Live wall") },
    { value: "playback", label: t("prefs.playback", "Playback") },
    { value: "alarms", label: t("prefs.alarms", "Alarms") },
    { value: "cameras", label: t("prefs.cameras", "Cameras") },
  ];

  return (
    <>
      <SettingsHeading description={t("prefs.sub", "These apply to your account on every device.")}>
        {t("prefs.title", "Preferences")}
      </SettingsHeading>
      <div className="divide-y divide-line">
        <SettingsRow
          label={t("prefs.clock24", "24-hour clock")}
          hint={t("prefs.clock24sub", "Show times as 14:30 rather than 2:30 PM")}
        >
          <Switch checked={prefs.clock24} onChange={(v) => setPref({ clock24: v })} label="24-hour clock" />
        </SettingsRow>

        <SettingsRow
          label={t("prefs.ribbon", "Weather ribbon on timeline")}
          hint={t("prefs.ribbonSub", "Show a temperature/precip trend line beside the playback timeline")}
        >
          <Switch
            checked={prefs.timelineRibbon}
            onChange={(v) => setPref({ timelineRibbon: v })}
            label="Weather ribbon"
          />
        </SettingsRow>

        {/* One opacity for every overlay on every video — first-party badges and
            extension OSD widgets alike. Live-previewed against a sample so the
            choice is made by looking at it rather than at a number. */}
        <SettingsRow
          label={t("prefs.osd", "Overlay opacity")}
          hint={t("prefs.osdSub", "How solid on-screen widgets look over the video")}
        >
          <div className="flex max-w-md items-center gap-4">
            <input
              type="range"
              min={OSD_MIN_OPACITY}
              max={OSD_MAX_OPACITY}
              step={0.05}
              value={prefs.osdOpacity}
              onChange={(e) => setPref({ osdOpacity: Number(e.target.value) })}
              className="min-w-0 flex-1"
              aria-label="Overlay opacity"
            />
            <span className="w-9 text-right text-sm tabular-nums text-fg">{Math.round(prefs.osdOpacity * 100)}%</span>
            {/* Sample tile: a scrap of "video" with a real OSD surface on it. */}
            <div className="relative h-9 w-16 shrink-0 overflow-hidden rounded-md border border-line bg-gradient-to-br from-ink-600 via-fg-faint to-ink-700">
              <div className="osd-surface absolute left-1 top-1 rounded px-1.5 py-0.5 text-[10px] text-white">12:34</div>
            </div>
          </div>
        </SettingsRow>

        <SettingsRow label={t("prefs.language", "Language")}>
          <select
            className="input max-w-[240px]"
            value={prefs.language}
            onChange={(e) => setPref({ language: e.target.value })}
            aria-label={t("prefs.language", "Language")}
          >
            <option value="auto">{t("prefs.auto", "Automatic")}</option>
            <option value="en">{t("prefs.english", "English")}</option>
            <option value="de">{t("prefs.german", "Deutsch")}</option>
          </select>
        </SettingsRow>

        <SettingsRow label={t("prefs.openTo", "Open to")}>
          <select
            className="input max-w-[240px]"
            value={prefs.landing}
            onChange={(e) => setPref({ landing: e.target.value as Preferences["landing"] })}
            aria-label={t("prefs.openTo", "Open to")}
          >
            {landings.map((l) => (
              <option key={l.value} value={l.value}>
                {l.label}
              </option>
            ))}
          </select>
        </SettingsRow>

        <SettingsRow label={t("prefs.span", "Default timeline span")}>
          <select
            className="input max-w-[240px]"
            value={prefs.timelineZoom}
            onChange={(e) => setPref({ timelineZoom: Number(e.target.value) })}
            aria-label={t("prefs.span", "Default timeline span")}
          >
            {ZOOM_PRESETS.map((z) => (
              <option key={z.idx} value={z.idx}>
                {z.label}
              </option>
            ))}
          </select>
        </SettingsRow>

        <SettingsRow label={t("prefs.density", "Density")}>
          <Segmented
            value={prefs.density}
            onChange={(d) => setPref({ density: d })}
            options={[
              { value: "comfortable", label: t("prefs.comfortable", "Comfortable") },
              { value: "compact", label: t("prefs.compact", "Compact") },
            ]}
          />
        </SettingsRow>
      </div>
    </>
  );
}
