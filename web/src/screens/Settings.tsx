import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { ExtensionSlot } from "../ext/ExtensionSlot";
import { ExtensionsManager } from "../ext/ExtensionsManager";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Plus, UserPlus, Shield, User as UserIcon } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { UserModal } from "../components/UserModal";
import { StorageManager } from "../components/StorageManager";
import { EufyBridge } from "../components/EufyBridge";
import { HomeAssistant } from "../components/HomeAssistant";
import { LiveViewCard } from "../components/LiveViewCard";
import { Interop } from "../components/Interop";
import { LocationWeather } from "../components/LocationWeather";
import { SystemHealth } from "../components/SystemHealth";
import { OidcSettings } from "../components/OidcSettings";
import { PreferencesSettings } from "../components/PreferencesSettings";
import { SecuritySettings } from "../components/SecuritySettings";
import { NotificationsSettings } from "../components/NotificationsSettings";
import { DetectionSettings } from "../components/DetectionSettings";
import { CamerasManager } from "./Cameras";
import { ApiTokens } from "../components/ApiTokens";
import { Webhooks } from "../components/Webhooks";
import { Automations } from "../components/Automations";
import { InteropEgress } from "../components/InteropEgress";
import { AuditLog } from "../components/AuditLog";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { useUsers, type ManagedUser } from "../hooks/useUsers";
import { useSiteName, useSetSiteName, useSetSettings, useHA, useHASensors } from "../hooks/useSystem";
import { Switch } from "../components/ui/Switch";
import { CameraGroupsManager } from "../components/CameraGroupsManager";

export function Settings() {
  const { t } = useTranslation();
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const { data: ver } = useQuery({
    queryKey: ["version"],
    queryFn: () => api.get<{ version: string; commit: string }>("/api/system/version"),
  });

  const tabs = [
    { id: "general", label: t("settings.tabGeneral", "General"), admin: false },
    { id: "security", label: t("settings.tabSecurity", "Security"), admin: false },
    { id: "notifications", label: t("settings.tabNotifications", "Notifications"), admin: false },
    { id: "cameras", label: t("settings.tabCameras", "Cameras"), admin: true },
    { id: "groups", label: t("settings.tabGroups", "Camera Groups"), admin: true },
    { id: "system", label: t("settings.tabSystem", "System"), admin: true },
    { id: "environment", label: t("settings.tabEnvironment", "Environment"), admin: true },
    { id: "detection", label: t("settings.tabDetection", "Detection"), admin: true },
    { id: "automations", label: t("settings.tabAutomations", "Automations"), admin: true },
    { id: "integrations", label: t("settings.tabIntegrations", "Integrations"), admin: true },
    { id: "extensions", label: t("settings.tabExtensions", "Extensions"), admin: true },
    { id: "activity", label: t("settings.tabActivity", "Activity"), admin: true },
  ].filter((tb) => !tb.admin || isAdmin);

  const [params] = useSearchParams();
  const [tab, setTab] = useState(params.get("tab") || "general");
  const active = tabs.some((tb) => tb.id === tab) ? tab : "general";

  return (
    <>
      <TopBar title={t("settings.title", "Settings")} />
      <div className="flex-1 overflow-auto">
        {/* Tab bar */}
        <div className="sticky top-0 z-10 border-b border-line bg-ink-900/95 backdrop-blur">
          <div className="max-w-2xl mx-auto flex gap-1 overflow-x-auto px-4 [scrollbar-width:none] [-ms-overflow-style:none] [&::-webkit-scrollbar]:hidden lg:max-w-none lg:justify-center lg:gap-2 lg:px-8">
            {tabs.map((tb) => (
              <button
                key={tb.id}
                onClick={() => setTab(tb.id)}
                className={`shrink-0 px-3 py-2.5 text-sm border-b-2 transition-colors lg:px-4 ${
                  active === tb.id
                    ? "border-brand text-fg"
                    : "border-transparent text-fg-muted hover:text-fg"
                }`}
              >
                {tb.label}
              </button>
            ))}
          </div>
        </div>

        <div className="p-4">
          <div className="max-w-2xl mx-auto space-y-4">
            {active === "general" && (
              <>
                <Section title={t("settings.account", "Account")}>
                  <Row label={t("settings.name", "Name")} value={user?.name || "—"} />
                  <Row label={t("settings.email", "Email")} value={user?.email || "—"} />
                  <Row label={t("settings.role", "Role")} value={user?.role || "—"} className="capitalize" />
                </Section>
                <PreferencesSettings />
                {isAdmin && <SiteSection />}
                <Section title={t("settings.about", "About")}>
                  <Row label={t("settings.application", "Application")} value="Aperture NVR" />
                  <Row label={t("settings.version", "Version")} value={ver ? `${ver.version} (${ver.commit})` : "…"} />
                </Section>
              </>
            )}

            {active === "security" && (
              <>
                <SecuritySettings />
                {isAdmin && <UsersSection currentUserId={user?.id} />}
              </>
            )}

            {active === "notifications" && <NotificationsSettings />}

            {active === "cameras" && isAdmin && <CamerasManager />}

            {active === "groups" && isAdmin && <CameraGroupsManager />}

            {active === "system" && isAdmin && (
              <>
                <SystemHealth />
                <StorageManager />
              </>
            )}

            {active === "environment" && isAdmin && <LocationWeather />}

            {active === "detection" && isAdmin && <DetectionSettings />}

            {active === "automations" && isAdmin && <Automations />}

            {active === "integrations" && isAdmin && (
              <>
                <HomeAssistant />
                <EufyBridge />
                <LiveViewCard />
                <Interop />
                <Webhooks />
                <ApiTokens />
                <InteropEgress />
                <OidcSettings />
              </>
            )}

            {active === "extensions" && isAdmin && <ExtensionsManager />}

            {active === "activity" && isAdmin && <AuditLog />}

            {/* Extension-contributed settings panels (append-only; nothing when none). */}
            <ExtensionSlot slot="settings" />
          </div>
        </div>
      </div>
    </>
  );
}

function UsersSection({ currentUserId }: { currentUserId?: string }) {
  const { data: users } = useUsers();
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<ManagedUser | null>(null);

  return (
    <div className="panel">
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-sm font-medium text-fg">Users &amp; roles</h2>
        <button
          className="btn-subtle h-8"
          onClick={() => {
            setEditing(null);
            setOpen(true);
          }}
        >
          <Plus className="h-4 w-4" />
          Add user
        </button>
      </div>
      <div className="space-y-1.5">
        {(users ?? []).map((u) => (
          <button
            key={u.id}
            onClick={() => {
              setEditing(u);
              setOpen(true);
            }}
            className="w-full text-left flex items-center gap-3 rounded-md px-2.5 py-2 hover:bg-ink-700"
          >
            <div className="h-8 w-8 rounded-full bg-ink-700 grid place-items-center shrink-0">
              {u.role === "admin" ? (
                <Shield className="h-4 w-4 text-brand" />
              ) : (
                <UserIcon className="h-4 w-4 text-fg-muted" />
              )}
            </div>
            <div className="min-w-0 flex-1">
              <p className="text-sm text-fg truncate">
                {u.name || u.email}
                {u.id === currentUserId && <span className="text-fg-faint"> (you)</span>}
              </p>
              <p className="text-xs text-fg-faint truncate">{u.email}</p>
            </div>
            {u.disabled && <span className="pill bg-ink-700 text-fg-faint">Disabled</span>}
            <span className="pill bg-ink-700 text-fg-muted capitalize">{u.role}</span>
            {u.role === "viewer" && (
              <span className="text-xs text-fg-faint w-16 text-right">
                {u.cameraIds.length} {u.cameraIds.length === 1 ? "camera" : "cameras"}
              </span>
            )}
          </button>
        ))}
        {(users ?? []).length === 0 && (
          <div className="flex flex-col items-center gap-1.5 py-6 text-fg-faint">
            <UserPlus className="h-5 w-5" />
            <span className="text-xs">No users yet</span>
          </div>
        )}
      </div>

      <UserModal open={open} user={editing} currentUserId={currentUserId} onClose={() => setOpen(false)} />
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
    <div className="panel">
      <h2 className="text-sm font-medium text-fg mb-3">System</h2>
      <label className="label">Site name</label>
      <div className="flex gap-2">
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
          disabled={setSite.isPending || !name.trim()}
          onClick={async () => {
            await setSite.mutateAsync(name.trim());
            setSaved(true);
          }}
        >
          {setSite.isPending ? "Saving…" : "Save"}
        </button>
      </div>
      {saved && <p className="text-xs text-online mt-2">Saved.</p>}

      <div className="mt-4 border-t border-line pt-4">
        <label className="label">Overview temperature</label>
        <select
          className="input"
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
        <p className="text-[11px] text-fg-faint mt-1.5">
          Shown on the dashboard. Defaults to your location&apos;s weather; pick a Home Assistant sensor to use its
          reading instead.
        </p>
        {!ha?.enabled && <p className="text-[11px] text-fg-faint mt-1">Connect Home Assistant to use a sensor.</p>}
      </div>

      <div className="mt-4 border-t border-line pt-4">
        <label className="label">Recording quality</label>
        <select
          className="input"
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
        <p className="text-[11px] text-fg-faint mt-1.5">
          Which stream cameras record to disk. <span className="text-fg-muted">Prefer low</span> uses each camera&apos;s sub
          stream where it has one; cameras without a sub keep recording their main stream. Live view is unaffected. Applies in
          a few seconds — recorders restart.
        </p>
      </div>

      <div className="mt-4 flex items-center justify-between border-t border-line pt-4">
        <div>
          <p className="text-sm text-fg">Spotlights card</p>
          <p className="text-[11px] text-fg-faint">Show the Spotlights panel on the dashboard sidebar.</p>
        </div>
        <Switch
          checked={spotlights}
          onChange={(v) => {
            setSpotlights(v);
            setSettings.mutate({ spotlightsEnabled: v });
          }}
          label="Spotlights card"
        />
      </div>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="panel">
      <h2 className="text-sm font-medium text-fg mb-3">{title}</h2>
      <div className="space-y-2.5">{children}</div>
    </div>
  );
}

function Row({ label, value, className }: { label: string; value: string; className?: string }) {
  return (
    <div className="flex items-center justify-between text-sm">
      <span className="text-fg-muted">{label}</span>
      <span className={`text-fg ${className ?? ""}`}>{value}</span>
    </div>
  );
}
