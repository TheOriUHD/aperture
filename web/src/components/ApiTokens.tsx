import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Plus, Trash2, Copy, Check } from "lucide-react";
import { api } from "../lib/api";

interface ApiToken {
  id: string;
  name: string;
  prefix: string;
  createdAt: number;
  lastUsed: number;
  expiresAt: number;
}

function fmtAgo(ms: number) {
  const d = Date.now() - ms;
  const m = Math.floor(d / 60000);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

// Scoped, revocable Bearer tokens for the API / automation. A token inherits its
// creator's role and camera access, and is shown exactly once.
export function ApiTokens() {
  const qc = useQueryClient();
  const { data: tokens } = useQuery({
    queryKey: ["tokens"],
    queryFn: () => api.get<ApiToken[]>("/api/tokens"),
  });
  const [name, setName] = useState("");
  const [days, setDays] = useState(0);
  const [created, setCreated] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [busy, setBusy] = useState(false);

  async function create() {
    if (!name.trim()) return;
    setBusy(true);
    try {
      const r = await api.post<{ secret: string }>("/api/tokens", {
        name: name.trim(),
        expiresInDays: days,
      });
      setCreated(r.secret);
      setName("");
      qc.invalidateQueries({ queryKey: ["tokens"] });
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
        Bearer credentials for the API and automation (n8n, scripts, Home Assistant). A token inherits
        your role and camera access. Use it as{" "}
        <code className="text-fg-faint">Authorization: Bearer apt_…</code>. See{" "}
        <code className="text-fg-faint">/api/openapi.json</code>.
      </p>

      {created && (
        <div className="rounded-md border border-online/40 bg-online/10 p-3 space-y-2">
          <p className="text-xs text-fg">Copy your token now — it won't be shown again.</p>
          <div className="flex items-center gap-2">
            <code className="flex-1 truncate rounded bg-ink-900 px-2 py-1.5 text-xs font-mono text-fg">
              {created}
            </code>
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

      <div className="flex gap-2">
        <input
          className="input flex-1"
          placeholder="Token name (e.g. n8n)"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <select className="input w-32" value={days} onChange={(e) => setDays(Number(e.target.value))}>
          <option value={0}>No expiry</option>
          <option value={30}>30 days</option>
          <option value={90}>90 days</option>
          <option value={365}>1 year</option>
        </select>
        <button className="btn-primary h-9" onClick={create} disabled={busy || !name.trim()}>
          <Plus className="h-4 w-4" />
        </button>
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
