import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Plus, UserPlus, Shield, User as UserIcon } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { UserModal } from "../components/UserModal";
import { StorageManager } from "../components/StorageManager";
import { HomeAssistant } from "../components/HomeAssistant";
import { LocationWeather } from "../components/LocationWeather";
import { SystemHealth } from "../components/SystemHealth";
import { OidcSettings } from "../components/OidcSettings";
import { PreferencesSettings } from "../components/PreferencesSettings";
import { SecuritySettings } from "../components/SecuritySettings";
import { NotificationsSettings } from "../components/NotificationsSettings";
import { AuditLog } from "../components/AuditLog";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { useUsers, type ManagedUser } from "../hooks/useUsers";
import { useSiteName, useSetSiteName } from "../hooks/useSystem";

export function Settings() {
  const { t } = useTranslation();
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const { data: ver } = useQuery({
    queryKey: ["version"],
    queryFn: () => api.get<{ version: string; commit: string }>("/api/system/version"),
  });

  return (
    <>
      <TopBar title={t("settings.title", "Settings")} />
      <div className="flex-1 overflow-auto p-4">
        <div className="max-w-2xl mx-auto space-y-4">
          <Section title={t("settings.account", "Account")}>
            <Row label={t("settings.name", "Name")} value={user?.name || "—"} />
            <Row label={t("settings.email", "Email")} value={user?.email || "—"} />
            <Row label={t("settings.role", "Role")} value={user?.role || "—"} className="capitalize" />
          </Section>

          {isAdmin && <SystemHealth />}

          <SecuritySettings />
          <PreferencesSettings />
          <NotificationsSettings />

          {isAdmin && <SiteSection />}
          {isAdmin && <LocationWeather />}
          {isAdmin && <UsersSection currentUserId={user?.id} />}
          {isAdmin && <OidcSettings />}
          {isAdmin && <StorageManager />}
          {isAdmin && <HomeAssistant />}
          {isAdmin && <AuditLog />}

          <Section title={t("settings.about", "About")}>
            <Row label={t("settings.application", "Application")} value="Aperture NVR" />
            <Row label={t("settings.version", "Version")} value={ver ? `${ver.version} (${ver.commit})` : "…"} />
          </Section>
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
  const [name, setName] = useState("");
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    if (site?.siteName) setName(site.siteName);
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
