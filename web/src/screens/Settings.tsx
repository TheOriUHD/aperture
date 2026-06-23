import { useQuery } from "@tanstack/react-query";
import { TopBar } from "../components/TopBar";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";

export function Settings() {
  const { user } = useAuth();
  const { data: ver } = useQuery({
    queryKey: ["version"],
    queryFn: () => api.get<{ version: string; commit: string }>("/api/system/version"),
  });

  return (
    <>
      <TopBar title="Settings" />
      <div className="flex-1 overflow-auto p-4">
        <div className="max-w-2xl mx-auto space-y-4">
          <Section title="Account">
            <Row label="Name" value={user?.name || "—"} />
            <Row label="Email" value={user?.email || "—"} />
            <Row label="Role" value={user?.role || "—"} className="capitalize" />
          </Section>

          {user?.role === "admin" && (
            <Section title="Users & roles">
              <p className="text-sm text-fg-faint">
                User management, per-camera access, and OIDC/SSO arrive in a later phase.
              </p>
            </Section>
          )}

          <Section title="About">
            <Row label="Application" value="Aperture NVR" />
            <Row label="Version" value={ver ? `${ver.version} (${ver.commit})` : "…"} />
          </Section>
        </div>
      </div>
    </>
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
