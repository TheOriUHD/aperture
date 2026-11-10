import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Monitor, Smartphone, Tablet, ShieldCheck, LogOut } from "lucide-react";
import { StatusDot } from "./ui/Layout";
import { Skeleton } from "./ui/Skeleton";
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
    <>
      <div className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2 pt-6">
        <div className="min-w-0">
          <h3 className="text-sm font-semibold text-fg">Signed-in devices</h3>
          <p className="mt-1 text-xs leading-relaxed text-fg-faint">
            You stay signed in until a device goes unused for 90 days.
            {user?.twoFactor && " A trusted device skips the code when you sign in there again."}
          </p>
        </div>
        {(others > 0 || otherTrust > 0) && (
          <button
            className="btn-ghost shrink-0"
            disabled={busy !== ""}
            onClick={() => run("others", () => api.post("/api/auth/sessions/revoke-others"))}
          >
            <LogOut className="h-4 w-4" />
            {busy === "others" ? "Signing out…" : "Sign out all others"}
          </button>
        )}
      </div>
      {err && <p className="text-xs text-danger">{err}</p>}
      {q.isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-10" />
          <Skeleton className="h-10" />
        </div>
      ) : rows.length > 0 ? (
        <div className="overflow-x-auto">
          <table className="tbl [&_td:first-child]:pl-0 [&_td:last-child]:pr-0 [&_th:first-child]:pl-0 [&_th:last-child]:pr-0">
            <thead>
              <tr>
                <th>Device</th>
                <th className="hidden sm:table-cell">Last active</th>
                <th className="hidden md:table-cell">IP address</th>
                <th className="hidden md:table-cell">Signed in</th>
                <th className="w-10" aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => {
                const d = describeDevice(s.userAgent);
                const Icon = icons[d.kind];
                return (
                  <tr key={s.id} className="hover:bg-white/[0.02]">
                    <td className="py-2">
                      <div className="flex min-w-0 items-start gap-2.5">
                        <Icon className="mt-0.5 h-4 w-4 shrink-0 text-fg-muted" />
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                            <span className="text-fg">{d.label}</span>
                            {s.current && <span className="pill-blue">This device</span>}
                            {s.trusted && user?.twoFactor && (
                              <span className="pill-gray" title="Skips the authenticator code at sign-in">
                                <ShieldCheck className="h-3 w-3" /> Trusted
                              </span>
                            )}
                          </div>
                          {/* On a phone the detail columns fold into one line under the name. */}
                          <p className="mt-0.5 truncate text-xs text-fg-faint sm:hidden">
                            {s.current ? "active now" : lastActive(s.lastSeenAtMs)}
                            {s.ip && ` · ${s.ip}`}
                          </p>
                        </div>
                      </div>
                    </td>
                    <td className="hidden whitespace-nowrap sm:table-cell">
                      <span className="inline-flex items-center gap-2">
                        {(s.current || Date.now() - s.lastSeenAtMs < 3_600_000) && <StatusDot tone="ok" />}
                        <span className="inline-block first-letter:uppercase">
                          {s.current ? "active now" : lastActive(s.lastSeenAtMs)}
                        </span>
                      </span>
                    </td>
                    <td className="hidden whitespace-nowrap tabular-nums md:table-cell">{s.ip || "—"}</td>
                    <td className="hidden whitespace-nowrap tabular-nums md:table-cell">
                      {new Date(s.createdAtMs).toLocaleDateString()}
                    </td>
                    <td className="w-10 text-right">
                      <button
                        className="btn-icon hover:text-danger disabled:opacity-45"
                        title={s.current ? "Sign out of this device" : "Sign this device out and forget it"}
                        aria-label={s.current ? "Sign out of this device" : "Sign this device out and forget it"}
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
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : q.isError ? (
        <p className="text-xs text-danger">Couldn’t load the signed-in devices.</p>
      ) : (
        <p className="flex items-center gap-3 border-y border-line py-4 text-sm text-fg-faint">
          <Monitor className="h-4 w-4 shrink-0" />
          No signed-in devices.
        </p>
      )}
    </>
  );
}
