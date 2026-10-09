import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Monitor, Smartphone, Tablet, ShieldCheck, LogOut } from "lucide-react";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { describeDevice } from "../lib/device";

interface SessionRow {
  id: string;
  createdAtMs: number;
  lastSeenAtMs: number;
  expiresAtMs: number;
  ip: string;
  userAgent: string;
  current: boolean;
  trusted: boolean; // skips the authenticator code at its next sign-in
}

interface SessionsResponse {
  sessions: SessionRow[];
  trustedDevices: number;
}

const icons = { phone: Smartphone, tablet: Tablet, desktop: Monitor };

// Coarse on purpose: activity is written back at most hourly.
function lastActive(ms: number): string {
  const mins = Math.round((Date.now() - ms) / 60_000);
  if (mins < 60) return "active now";
  const hours = Math.round(mins / 60);
  if (hours < 24) return `active ${hours} h ago`;
  const days = Math.round(hours / 24);
  if (days < 30) return `active ${days} day${days === 1 ? "" : "s"} ago`;
  return `active ${new Date(ms).toLocaleDateString()}`;
}

// The browsers signed in to this account. Sign-in lasts until a browser goes
// unused for 90 days, so this is where a lost or shared device is signed out.
export function SignedInDevices() {
  const { user, refresh } = useAuth();
  const q = useQuery({
    queryKey: ["sessions"],
    queryFn: () => api.get<SessionsResponse>("/api/auth/sessions"),
  });
  const [busy, setBusy] = useState("");
  const [err, setErr] = useState("");

  async function run(key: string, fn: () => Promise<unknown>) {
    setErr("");
    setBusy(key);
    try {
      await fn();
      await q.refetch();
    } catch (e) {
      setErr((e as Error).message || "That didn't work.");
    } finally {
      setBusy("");
    }
  }

  const rows = q.data?.sessions ?? [];
  const others = rows.filter((s) => !s.current).length;
  const otherTrust = (q.data?.trustedDevices ?? 0) - (rows.some((s) => s.current && s.trusted) ? 1 : 0);

  return (
    <div className="rounded-lg bg-ink-850 p-3.5 mb-4">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <Monitor className="h-5 w-5 text-fg-muted" />
          <div>
            <p className="text-sm text-fg">Signed-in devices</p>
            <p className="text-xs text-fg-faint">
              You stay signed in until a device goes unused for 90 days.
              {user?.twoFactor && " A trusted device skips the code when you sign in there again."}
            </p>
          </div>
        </div>
        {(others > 0 || otherTrust > 0) && (
          <button
            className="btn-subtle shrink-0"
            disabled={busy !== ""}
            onClick={() => run("others", () => api.post("/api/auth/sessions/revoke-others"))}
          >
            {busy === "others" ? "Signing out…" : "Sign out all others"}
          </button>
        )}
      </div>
      {err && <p className="text-xs text-rec mt-2">{err}</p>}
      {rows.length > 0 && (
        <div className="mt-3 space-y-1.5">
          {rows.map((s) => {
            const d = describeDevice(s.userAgent);
            const Icon = icons[d.kind];
            return (
              <div key={s.id} className="flex items-center gap-3 rounded-md bg-ink-800 px-3 py-2">
                <Icon className="h-4 w-4 text-fg-muted shrink-0" />
                <div className="min-w-0 flex-1">
                  <p className="text-sm text-fg truncate flex items-center gap-1.5">
                    {d.label}
                    {s.current && <span className="pill bg-brand/15 text-brand">This device</span>}
                    {s.trusted && user?.twoFactor && (
                      <span className="pill bg-ink-700 text-fg-muted" title="Skips the authenticator code at sign-in">
                        <ShieldCheck className="h-3 w-3" /> Trusted
                      </span>
                    )}
                  </p>
                  <p className="text-xs text-fg-faint truncate">
                    {s.current ? "active now" : lastActive(s.lastSeenAtMs)}
                    {s.ip && ` · ${s.ip}`}
                    {` · signed in ${new Date(s.createdAtMs).toLocaleDateString()}`}
                  </p>
                </div>
                <button
                  className="text-fg-faint hover:text-rec p-1 disabled:opacity-50"
                  title={s.current ? "Sign out of this device" : "Sign this device out and forget it"}
                  disabled={busy !== ""}
                  onClick={() =>
                    run(s.id, async () => {
                      await api.del(`/api/auth/sessions/${s.id}`);
                      if (s.current) await refresh(); // back to the sign-in screen
                    })
                  }
                >
                  <LogOut className="h-4 w-4" />
                </button>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
