import { useEffect, useRef, useState } from "react";
import { Link, Navigate, useSearchParams } from "react-router-dom";
import { ExtensionSlot } from "../ext/ExtensionSlot";
import { ExtensionsManager } from "../ext/ExtensionsManager";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import {
  Bell,
  Blocks,
  Cpu,
  ChevronRight,
  Cctv,
  CloudSun,
  HardDrive,
  LayoutGrid,
  Plug,
  ScanEye,
  ScrollText,
  Server,
  ShieldCheck,
  SlidersHorizontal,
  Users,
  Workflow,
  type LucideIcon,
} from "lucide-react";
import { TopBar } from "../components/TopBar";
import { StorageManager } from "../components/StorageManager";
import { LocationWeather } from "../components/LocationWeather";
import { SystemHealth } from "../components/SystemHealth";
import { PreferencesSettings } from "../components/PreferencesSettings";
import { SecuritySettings } from "../components/SecuritySettings";
import { NotificationsSettings } from "../components/NotificationsSettings";
import { DetectionSettings } from "../components/DetectionSettings";
import { CameraGroupsManager } from "../components/CameraGroupsManager";
import { HardwareSettings } from "../components/HardwareSettings";
import { SettingsHeading, SettingsRow } from "../components/ui/Layout";
import { Switch } from "../components/ui/Switch";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { useSiteName, useSetSiteName, useSetSettings, useHA, useHASensors } from "../hooks/useSystem";

interface Section {
  id: string;
  label: string;
  icon: LucideIcon;
  admin: boolean;
  group: "account" | "cameras" | "system";
}

// Sections that used to be settings tabs and are pages of their own now. Old
// links (?tab=...) still land in the right place.
const MOVED: Record<string, string> = {
  cameras: "/cameras",
  automations: "/automations",
  integrations: "/integrations",
  activity: "/logs",
  users: "/users",
};

export function Settings() {
  const { t } = useTranslation();
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const { data: ver } = useQuery({
    queryKey: ["version"],
    queryFn: () => api.get<{ version: string; commit: string }>("/api/system/version"),
  });

  const sections: Section[] = (
    [
      { id: "general", label: t("settings.tabGeneral", "General"), icon: SlidersHorizontal, admin: false, group: "account" },
      { id: "security", label: t("settings.tabSecurity", "Security"), icon: ShieldCheck, admin: false, group: "account" },
      { id: "notifications", label: t("settings.tabNotifications", "Notifications"), icon: Bell, admin: false, group: "account" },
      { id: "groups", label: t("settings.tabGroups", "Camera Groups"), icon: LayoutGrid, admin: true, group: "cameras" },
      { id: "detection", label: t("settings.tabDetection", "Detection"), icon: ScanEye, admin: true, group: "cameras" },
      { id: "environment", label: t("settings.tabEnvironment", "Environment"), icon: CloudSun, admin: true, group: "cameras" },
      { id: "storage", label: t("settings.tabStorage", "Recording & Storage"), icon: HardDrive, admin: true, group: "system" },
      { id: "system", label: t("settings.tabSystem", "System"), icon: Server, admin: true, group: "system" },
      { id: "hardware", label: t("settings.tabHardware", "Hardware"), icon: Cpu, admin: true, group: "system" },
      { id: "extensions", label: t("settings.tabExtensions", "Extensions"), icon: Blocks, admin: true, group: "system" },
    ] as Section[]
  ).filter((s) => !s.admin || isAdmin);

  const [params, setParams] = useSearchParams();
  const requested = params.get("tab") || "general";
  const [tab, setTabState] = useState(requested);
  useEffect(() => setTabState(requested), [requested]);
  const stripRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    stripRef.current?.querySelector<HTMLElement>("[data-active]")?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [tab]);
  const setTab = (id: string) => {
    setTabState(id);
    setParams({ tab: id }, { replace: true });
  };

  if (MOVED[requested]) return <Navigate to={MOVED[requested]} replace />;
  const active = sections.some((s) => s.id === tab) ? tab : "general";
  const current = sections.find((s) => s.id === active)!;
  const groups: { id: Section["group"]; label?: string }[] = [
    { id: "account" },
    { id: "cameras", label: t("settings.groupCameras", "Cameras") },
    { id: "system", label: t("settings.groupSystem", "System") },
  ];

  return (
    <>
      <TopBar title={t("settings.title", "Settings")} />
      <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
        {/* Section navigation: a side list on desktop, a scrolling strip on a phone. */}
        <nav className="hidden w-[248px] shrink-0 flex-col overflow-y-auto border-r border-line px-3 py-4 lg:flex">
          {groups.map((g) => {
            const items = sections.filter((s) => s.group === g.id);
            if (items.length === 0) return null;
            return (
              <div key={g.id} className={clsx(g.label && "mt-4 border-t border-line pt-4")}>
                {g.label && <p className="mb-1.5 px-3 text-xs font-semibold text-fg">{g.label}</p>}
                <div className="space-y-0.5">
                  {items.map((s) => (
                    <button
                      key={s.id}
                      onClick={() => setTab(s.id)}
                      className={clsx("side-item", active === s.id && "side-item-active")}
                    >
                      <s.icon className="h-[18px] w-[18px] shrink-0" strokeWidth={1.75} />
                      {s.label}
                    </button>
                  ))}
                </div>
              </div>
            );
          })}
          <p className="mt-auto px-3 pt-6 text-xs text-fg-faint">Aperture {ver?.version ?? ""}</p>
        </nav>
        <div className="shrink-0 border-b border-line lg:hidden">
          <div
            ref={stripRef}
            className="flex gap-1 overflow-x-auto px-3 py-2 [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
          >
            {sections.map((s) => (
              <button
                key={s.id}
                data-active={active === s.id ? "" : undefined}
                onClick={() => setTab(s.id)}
                className={clsx(
                  "flex h-8 shrink-0 items-center gap-1.5 rounded-md px-3 text-sm",
                  active === s.id ? "bg-ink-700 text-brand" : "text-fg-muted",
                )}
              >
                <s.icon className="h-4 w-4" />
                {s.label}
              </button>
            ))}
          </div>
        </div>

        <div className="min-w-0 flex-1 overflow-y-auto">
          <div className="max-w-[1080px] px-4 py-5 lg:px-8 lg:py-6">
            <h1 className="mb-4 text-base font-semibold text-fg lg:hidden">{current.label}</h1>
            <div className="space-y-4">
              {active === "general" && (
                <>
                  <SettingsHeading>{t("settings.account", "Account")}</SettingsHeading>
                  <div className="divide-y divide-line">
                    <SettingsRow label={t("settings.name", "Name")}>
                      <span className="text-sm text-fg">{user?.name || "—"}</span>
                    </SettingsRow>
                    <SettingsRow label={t("settings.email", "Email")}>
                      <span className="text-sm text-fg">{user?.email || "—"}</span>
                    </SettingsRow>
                    <SettingsRow label={t("settings.role", "Role")}>
                      <span className="pill-gray capitalize">{user?.role || "—"}</span>
                    </SettingsRow>
                  </div>
                  <PreferencesSettings />
                  {isAdmin && <SiteSection />}
                  <SettingsHeading>{t("settings.about", "About")}</SettingsHeading>
                  <div className="divide-y divide-line">
                    <SettingsRow label={t("settings.application", "Application")}>
                      <span className="text-sm text-fg">Aperture NVR</span>
                    </SettingsRow>
                    <SettingsRow label={t("settings.version", "Version")}>
                      <span className="font-mono text-xs text-fg/85">{ver ? `${ver.version} (${ver.commit})` : "…"}</span>
                    </SettingsRow>
                  </div>
                </>
              )}

              {active === "security" && <SecuritySettings />}

              {active === "notifications" && <NotificationsSettings />}

              {active === "groups" && isAdmin && <CameraGroupsManager />}

              {active === "detection" && isAdmin && <DetectionSettings />}

              {active === "environment" && isAdmin && <LocationWeather />}

              {active === "storage" && isAdmin && <StorageManager />}

              {active === "system" && isAdmin && <SystemHealth />}

              {active === "hardware" && isAdmin && <HardwareSettings />}

              {active === "extensions" && isAdmin && <ExtensionsManager />}

              {/* Extension-contributed settings panels (append-only; nothing when none). */}
              <ExtensionSlot slot="settings" />

              {/* On a phone there is no rail: the pages that left Settings are linked here. */}
              {isAdmin && active === "general" && <MoreLinks />}
            </div>
          </div>
        </div>
      </div>
    </>
  );
}

function MoreLinks() {
  const links: { to: string; label: string; icon: LucideIcon }[] = [
    { to: "/cameras", label: "Devices", icon: Cctv },
    { to: "/users", label: "Users", icon: Users },
    { to: "/integrations", label: "Integrations", icon: Plug },
    { to: "/automations", label: "Automations", icon: Workflow },
    { to: "/logs", label: "Activity Log", icon: ScrollText },
  ];
  return (
    <div className="lg:hidden">
      <SettingsHeading>Administration</SettingsHeading>
      <div className="mt-2 divide-y divide-line overflow-hidden rounded-lg border border-line">
        {links.map((l) => (
          <Link key={l.to} to={l.to} className="flex h-12 items-center gap-3 px-4 text-sm text-fg hover:bg-white/[0.03]">
            <l.icon className="h-[18px] w-[18px] text-fg-muted" />
            <span className="flex-1">{l.label}</span>
            <ChevronRight className="h-4 w-4 text-fg-faint" />
          </Link>
        ))}
      </div>
    </div>
  );
}

function SiteSection() {
  const { data: site } = useSiteName();
  const setSite = useSetSiteName();
  const setSettings = useSetSettings();
  const { data: ha } = useHA();
  const { data: sensors } = useHASensors(!!ha?.enabled);
  const [name, setName] = useState("");
  const [saved, setSaved] = useState(false);
  const [tempSensor, setTempSensor] = useState("");
  const [spotlights, setSpotlights] = useState(true);
  const [recordQuality, setRecordQuality] = useState<"high" | "low">("high");
  useEffect(() => {
    if (site) {
      setName(site.siteName);
      setTempSensor(site.overviewTempSensor || "");
      setSpotlights(site.spotlightsEnabled ?? true);
      setRecordQuality(site.recordQuality ?? "high");
    }
  }, [site]);

  return (
    <>
      <SettingsHeading>System</SettingsHeading>
      <div className="divide-y divide-line">
        <SettingsRow label="Site name" hint="Shown in the header and on notifications.">
          <div className="flex max-w-md gap-2">
            <input
              className="input"
              value={name}
              placeholder="Home NVR"
              onChange={(e) => {
                setName(e.target.value);
                setSaved(false);
              }}
            />
            <button
              className="btn-primary shrink-0"
              disabled={setSite.isPending || !name.trim() || name.trim() === site?.siteName}
              onClick={async () => {
                await setSite.mutateAsync(name.trim());
                setSaved(true);
              }}
            >
              {setSite.isPending ? "Saving…" : saved ? "Saved" : "Save"}
            </button>
          </div>
        </SettingsRow>

        <SettingsRow
          label="Overview temperature"
          align="start"
          hint={
            <>
              Shown on the dashboard. Defaults to your location&apos;s weather; pick a Home Assistant sensor to use its
              reading instead.{!ha?.enabled && " Connect Home Assistant to use a sensor."}
            </>
          }
        >
          <select
            className="input max-w-md"
            value={tempSensor}
            onChange={(e) => {
              setTempSensor(e.target.value);
              setSettings.mutate({ overviewTempSensor: e.target.value });
            }}
          >
            <option value="">Outdoor weather (default)</option>
            {tempSensor && !sensors?.some((s) => s.entityId === tempSensor) && (
              <option value={tempSensor}>{tempSensor} (waiting for reading…)</option>
            )}
            {sensors?.map((s) => (
              <option key={s.entityId} value={s.entityId}>
                {(s.name || s.entityId) + (s.unit ? ` — ${s.value} ${s.unit}` : ` — ${s.value}`)}
              </option>
            ))}
          </select>
        </SettingsRow>

        <SettingsRow
          label="Recording quality"
          align="start"
          hint={
            <>
              Which stream cameras record to disk. <span className="text-fg-muted">Prefer low</span> uses each camera&apos;s
              sub stream where it has one; cameras without a sub keep recording their main stream. Live view is unaffected.
              Applies in a few seconds — recorders restart.
            </>
          }
        >
          <select
            className="input max-w-md"
            value={recordQuality}
            onChange={(e) => {
              const v = e.target.value as "high" | "low";
              setRecordQuality(v);
              setSettings.mutate({ recordQuality: v });
            }}
          >
            <option value="high">Prefer high quality — main stream</option>
            <option value="low">Prefer low quality — sub stream (saves space)</option>
          </select>
        </SettingsRow>

        <SettingsRow label="Spotlights card" hint="Show the Spotlights panel in the dashboard overview.">
          <Switch
            checked={spotlights}
            onChange={(v) => {
              setSpotlights(v);
              setSettings.mutate({ spotlightsEnabled: v });
            }}
            label="Spotlights card"
          />
        </SettingsRow>
      </div>
    </>
  );
}
