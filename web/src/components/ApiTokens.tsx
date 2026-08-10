import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Plus, Trash2, Copy, Check } from "lucide-react";
import { api, ApiError } from "../lib/api";
import { useCameras } from "../hooks/useCameras";

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

  return (
    <div className="panel space-y-4">
      <div className="flex items-center gap-2">
        <KeyRound className="h-4 w-4 text-fg-muted" />
        <h2 className="text-sm font-medium text-fg">API tokens</h2>
      </div>
      <p className="text-xs text-fg-muted leading-relaxed">
        Bearer credentials for the API and automation (n8n, scripts, Home Assistant). A token can never
        exceed your role or camera access, and carries only the scopes you grant. Use it as{" "}
        <code className="text-fg-faint">Authorization: Bearer apt_…</code>. For the Home Assistant live
        card, grant <code className="text-fg-faint">media:live</code> and restrict it to that camera.
      </p>

      {created && (
        <div className="rounded-md border border-online/40 bg-online/10 p-3 space-y-2">
          <p className="text-xs text-fg">Copy your token now — it won't be shown again.</p>
          <div className="flex items-center gap-2">
            <code className="flex-1 truncate rounded bg-ink-900 px-2 py-1.5 text-xs font-mono text-fg">{created}</code>
            <button
              className="btn-ghost h-8"
              onClick={() => {
                void navigator.clipboard?.writeText(created);
                setCopied(true);
                setTimeout(() => setCopied(false), 1500);
              }}
            >
              {copied ? <Check className="h-4 w-4 text-online" /> : <Copy className="h-4 w-4" />}
            </button>
          </div>
          <button className="text-xs text-fg-faint hover:text-fg" onClick={() => setCreated(null)}>
            Dismiss
          </button>
        </div>
      )}

      {/* Create form */}
      <div className="space-y-3 rounded-lg border border-line bg-ink-900/40 p-3">
        <div className="flex gap-2">
          <input className="input flex-1" placeholder="Token name (e.g. ha-live-card)" value={name} onChange={(e) => setName(e.target.value)} />
          <select className="input w-32" value={days} onChange={(e) => setDays(Number(e.target.value))}>
            <option value={0}>No expiry</option>
            <option value={30}>30 days</option>
            <option value={90}>90 days</option>
            <option value={365}>1 year</option>
          </select>
        </div>

        <div>
          <div className="mb-1.5 flex items-center justify-between">
            <p className="label mb-0">Scopes {scopes.size > 0 && <span className="text-fg-faint">· {scopes.size} selected</span>}</p>
            <div className="flex gap-2 text-[11px]">
              <button type="button" className="text-brand hover:underline" onClick={() => setScopes(new Set(["media:live"]))}>
                Live view only
              </button>
              {scopes.size > 0 && (
                <button type="button" className="text-fg-muted hover:underline" onClick={() => setScopes(new Set())}>
                  Clear
                </button>
              )}
            </div>
          </div>
          <div className="max-h-44 space-y-2 overflow-auto rounded-md border border-line bg-ink-850 p-2.5">
            {SCOPE_GROUPS.map((g) => (
              <div key={g.label}>
                <p className="mb-1 text-[10px] uppercase tracking-wide text-fg-faint">{g.label}</p>
                <div className="grid grid-cols-1 gap-1 sm:grid-cols-2">
                  {g.scopes.map(([scope, label]) => (
                    <label key={scope} className="flex cursor-pointer items-center gap-2 text-xs text-fg-muted hover:text-fg">
                      <input type="checkbox" className="accent-brand" checked={scopes.has(scope)} onChange={() => toggle(scopes, scope, setScopes)} />
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
          <label className="flex cursor-pointer items-center gap-2 text-xs text-fg-muted">
            <input type="checkbox" className="accent-brand" checked={restrict} onChange={(e) => setRestrict(e.target.checked)} />
            Restrict to specific cameras <span className="text-fg-faint">(default: all cameras you can see)</span>
          </label>
          {restrict && (
            <div className="mt-2 grid max-h-32 grid-cols-1 gap-1 overflow-auto rounded-md border border-line bg-ink-850 p-2.5 sm:grid-cols-2">
              {cameras.length === 0 && <p className="text-[11px] text-fg-faint">No cameras.</p>}
              {cameras.map((c) => (
                <label key={c.id} className="flex cursor-pointer items-center gap-2 text-xs text-fg-muted hover:text-fg">
                  <input type="checkbox" className="accent-brand" checked={cams.has(c.id)} onChange={() => toggle(cams, c.id, setCams)} />
                  <span className="truncate">{c.name || c.id}</span>
                </label>
              ))}
            </div>
          )}
        </div>

        {error && <p className="text-xs text-rec">{error}</p>}

        <div className="flex items-center justify-between">
          <p className="text-[11px] text-fg-faint">{scopes.size === 0 ? "Select at least one scope." : restrict && cams.size === 0 ? "Pick at least one camera, or untick to allow all." : " "}</p>
          <button className="btn-primary h-9" onClick={create} disabled={!canCreate || (restrict && cams.size === 0)}>
            <Plus className="h-4 w-4" /> {busy ? "Creating…" : "Create token"}
          </button>
        </div>
      </div>

      <div className="divide-y divide-line">
        {(tokens ?? []).length === 0 && <p className="py-2 text-xs text-fg-faint">No tokens yet.</p>}
        {(tokens ?? []).map((t) => (
          <div key={t.id} className="flex items-center gap-3 py-2">
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm text-fg">{t.name}</p>
              <p className="font-mono text-[11px] text-fg-faint">
                {t.prefix}… · {t.lastUsed ? `used ${fmtAgo(t.lastUsed)}` : "never used"}
                {t.expiresAt ? ` · expires ${new Date(t.expiresAt).toLocaleDateString()}` : ""}
              </p>
            </div>
            <button className="btn-icon h-8 w-8" onClick={() => revoke(t.id)} aria-label="Revoke token">
              <Trash2 className="h-4 w-4" />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
