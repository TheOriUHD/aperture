import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Plus, Trash2, Check } from "lucide-react";
import { api, ApiError } from "../lib/api";
import { useCameras } from "../hooks/useCameras";
import { EmptyState } from "./ui/EmptyState";
import { Callout, CodeValue, CopyIconButton, IntegrationHeader } from "./IntegrationParts";

interface ApiToken {
  id: string;
  name: string;
  prefix: string;
  createdAt: number;
  lastUsed: number;
  expiresAt: number;
}

// The full assignable scope taxonomy (mirrors internal/api/scopes.go), grouped and
// labelled so a token's authority is chosen deliberately. A token must carry at least
// one scope — the backend rejects an empty-scope token as useless.
const SCOPE_GROUPS: { label: string; scopes: [string, string][] }[] = [
  { label: "Live & media", scopes: [["media:live", "Live streams (WebRTC/HLS)"], ["media:read", "Recorded playback"], ["media:write", "Exports & shares"]] },
  { label: "Cameras & events", scopes: [["cameras:read", "Read cameras"], ["cameras:write", "Manage cameras"], ["events:read", "Read events"], ["events:write", "Manage events"]] },
  { label: "Coverage & segments", scopes: [["coverage:read", "Recording coverage"], ["segments:read", "Raw segments"]] },
  { label: "Storage & schedules", scopes: [["storage:read", "Read storage"], ["storage:write", "Manage storage"], ["schedules:read", "Read schedules"], ["schedules:write", "Manage schedules"]] },
  { label: "Zones & detection", scopes: [["zones:read", "Read zones"], ["zones:write", "Manage zones"], ["detection:read", "Read detection"], ["detection:write", "Manage detection"]] },
  { label: "Automation & alarm", scopes: [["automation:read", "Read automations"], ["automation:write", "Manage automations"], ["alarm:arm", "Arm/disarm alarm"]] },
  { label: "Site & system", scopes: [["site:read", "Read site settings"], ["site:write", "Manage site"], ["system:read", "System health"]] },
];

function fmtAgo(ms: number) {
  const d = Date.now() - ms;
  const m = Math.floor(d / 60000);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

// Scoped, revocable Bearer tokens for the API / automation. A token inherits its
// creator's role and camera access, narrowed to the scopes (and optionally cameras)
// chosen here, and is shown exactly once.
export function ApiTokens() {
  const qc = useQueryClient();
  const { data: tokens } = useQuery({
    queryKey: ["tokens"],
    queryFn: () => api.get<ApiToken[]>("/api/tokens"),
  });
  const { data: cameras = [] } = useCameras();

  const [name, setName] = useState("");
  const [days, setDays] = useState(0);
  const [scopes, setScopes] = useState<Set<string>>(new Set());
  const [restrict, setRestrict] = useState(false);
  const [cams, setCams] = useState<Set<string>>(new Set());
  const [created, setCreated] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [formOpen, setFormOpen] = useState(false);

  const toggle = (set: Set<string>, key: string, setter: (s: Set<string>) => void) => {
    const next = new Set(set);
    next.has(key) ? next.delete(key) : next.add(key);
    setter(next);
  };

  const canCreate = !!name.trim() && scopes.size > 0 && !busy;

  async function create() {
    if (!canCreate) return;
    setBusy(true);
    setError(undefined);
    try {
      const r = await api.post<{ secret: string }>("/api/tokens", {
        name: name.trim(),
        expiresInDays: days,
        scopes: [...scopes],
        cameraGrants: restrict ? [...cams] : [], // empty = all cameras the owner can see
      });
      setCreated(r.secret);
      setName("");
      setScopes(new Set());
      setRestrict(false);
      setCams(new Set());
      setFormOpen(false);
      qc.invalidateQueries({ queryKey: ["tokens"] });
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Could not create token.");
    } finally {
      setBusy(false);
    }
  }

  async function revoke(id: string) {
    await api.del(`/api/tokens/${id}`);
    qc.invalidateQueries({ queryKey: ["tokens"] });
  }

  const list = tokens ?? [];

  return (
    <div>
      <IntegrationHeader
        icon={KeyRound}
        title="API tokens"
        action={
          !formOpen && (
            <button className="btn-outline" onClick={() => setFormOpen(true)}>
              <Plus className="h-4 w-4" /> Create token
            </button>
          )
        }
      >
        Bearer credentials for the API and automation (n8n, scripts, Home Assistant). A token can never
        exceed your role or camera access, and carries only the scopes you grant. Use it as{" "}
        <code className="text-fg">Authorization: Bearer apt_…</code>. For the Home Assistant live
        card, grant <code className="text-fg">media:live</code> and restrict it to that camera.
      </IntegrationHeader>

      {created && (
        <Callout tone="ok" icon={Check} className="mb-4 mt-2">
          <p className="text-sm text-fg">Copy your token now — it won't be shown again.</p>
          <div className="mt-2 flex max-w-2xl items-center gap-1">
            <CodeValue className="flex-1 text-fg">{created}</CodeValue>
            <CopyIconButton
              copied={copied}
              label="Copy token"
              onClick={() => {
                void navigator.clipboard?.writeText(created);
                setCopied(true);
                setTimeout(() => setCopied(false), 1500);
              }}
            />
          </div>
          <button className="btn-link mt-2 text-xs" onClick={() => setCreated(null)}>
            Dismiss
          </button>
        </Callout>
      )}

      {/* Create form */}
      {formOpen && (
        <div className="mb-4 mt-2 rounded-lg border border-line p-4">
          <p className="mb-3 text-sm font-semibold text-fg">New token</p>
          <div className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_160px]">
              <div>
                <label className="label">Name</label>
                <input className="input" placeholder="Token name (e.g. ha-live-card)" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
              </div>
              <div>
                <label className="label">Expires</label>
                <select className="input" value={days} onChange={(e) => setDays(Number(e.target.value))}>
                  <option value={0}>No expiry</option>
                  <option value={30}>30 days</option>
                  <option value={90}>90 days</option>
                  <option value={365}>1 year</option>
                </select>
              </div>
            </div>

            <div>
              <div className="mb-1.5 flex items-center justify-between">
                <p className="label mb-0">Scopes {scopes.size > 0 && <span className="text-fg-faint">· {scopes.size} selected</span>}</p>
                <div className="flex gap-3 text-xs">
                  <button type="button" className="text-brand hover:text-brand-hover" onClick={() => setScopes(new Set(["media:live"]))}>
                    Live view only
                  </button>
                  {scopes.size > 0 && (
                    <button type="button" className="text-fg-muted hover:text-fg" onClick={() => setScopes(new Set())}>
                      Clear
                    </button>
                  )}
                </div>
              </div>
              <div className="space-y-3 rounded-md border border-line bg-ink-850 p-3">
                {SCOPE_GROUPS.map((g) => (
                  <div key={g.label}>
                    <p className="mb-1 text-[11px] font-semibold text-fg-muted">{g.label}</p>
                    <div className="grid grid-cols-1 gap-x-4 sm:grid-cols-2">
                      {g.scopes.map(([scope, label]) => (
                        <label key={scope} className="flex h-7 cursor-pointer items-center gap-2 rounded px-1 text-sm text-fg/85 hover:bg-white/[0.03]">
                          <input type="checkbox" checked={scopes.has(scope)} onChange={() => toggle(scopes, scope, setScopes)} />
                          <span className="truncate">{label}</span>
                          <code className="ml-auto shrink-0 text-[10px] text-fg-faint">{scope}</code>
                        </label>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </div>

            <div>
              <label className="flex cursor-pointer items-center gap-2 text-sm text-fg/85">
                <input type="checkbox" checked={restrict} onChange={(e) => setRestrict(e.target.checked)} />
                Restrict to specific cameras <span className="text-xs text-fg-faint">(default: all cameras you can see)</span>
              </label>
              {restrict && (
                <div className="mt-2 grid max-h-40 grid-cols-1 gap-x-4 overflow-auto rounded-md border border-line bg-ink-850 p-3 sm:grid-cols-2">
                  {cameras.length === 0 && <p className="text-xs text-fg-faint">No cameras.</p>}
                  {cameras.map((c) => (
                    <label key={c.id} className="flex h-7 cursor-pointer items-center gap-2 rounded px-1 text-sm text-fg/85 hover:bg-white/[0.03]">
                      <input type="checkbox" checked={cams.has(c.id)} onChange={() => toggle(cams, c.id, setCams)} />
                      <span className="truncate">{c.name || c.id}</span>
                    </label>
                  ))}
                </div>
              )}
            </div>

            {error && <p className="text-sm text-danger">{error}</p>}

            <div className="flex flex-wrap items-center justify-end gap-x-3 gap-y-2 border-t border-line pt-4">
              <p className="mr-auto text-xs text-fg-faint">{scopes.size === 0 ? "Select at least one scope." : restrict && cams.size === 0 ? "Pick at least one camera, or untick to allow all." : " "}</p>
              <button className="btn-ghost" onClick={() => setFormOpen(false)}>
                Cancel
              </button>
              <button className="btn-primary" onClick={create} disabled={!canCreate || (restrict && cams.size === 0)}>
                <Plus className="h-4 w-4" /> {busy ? "Creating…" : "Create token"}
              </button>
            </div>
          </div>
        </div>
      )}

      {list.length === 0 ? (
        !formOpen && (
          <EmptyState icon={KeyRound} title="No tokens yet." />
        )
      ) : (
        <div className="mt-2 overflow-x-auto">
          <table className="tbl">
            <thead>
              <tr>
                <th>Name</th>
                <th>API key</th>
                <th className="hidden md:table-cell">Created</th>
                <th className="hidden sm:table-cell">Last used</th>
                <th className="hidden sm:table-cell">Expires</th>
                <th className="w-px" aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {list.map((t) => (
                <tr key={t.id}>
                  <td className="max-w-[240px]">
                    <span className="block truncate text-fg">{t.name}</span>
                    <span className="block truncate text-[11px] text-fg-faint sm:hidden">
                      {t.lastUsed ? `used ${fmtAgo(t.lastUsed)}` : "never used"}
                      {t.expiresAt ? ` · expires ${new Date(t.expiresAt).toLocaleDateString()}` : ""}
                    </span>
                  </td>
                  <td className="whitespace-nowrap font-mono text-xs">{t.prefix}…</td>
                  <td className="hidden whitespace-nowrap md:table-cell">
                    {t.createdAt ? new Date(t.createdAt).toLocaleDateString() : "–"}
                  </td>
                  <td className="hidden whitespace-nowrap sm:table-cell">{t.lastUsed ? fmtAgo(t.lastUsed) : "Never"}</td>
                  <td className="hidden whitespace-nowrap sm:table-cell">
                    {t.expiresAt ? new Date(t.expiresAt).toLocaleDateString() : "Never"}
                  </td>
                  <td className="!px-1">
                    <button className="btn-icon hover:text-danger" onClick={() => revoke(t.id)} aria-label="Revoke token" title="Revoke token">
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
