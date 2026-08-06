import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Webhook as WebhookIcon, Plus, Trash2, Send } from "lucide-react";
import { api } from "../lib/api";

interface Webhook {
  id: string;
  name: string;
  url: string;
  events: string[];
  secret?: string;
  enabled: boolean;
  createdAt: number;
  etag?: string; // per-item concurrency tag from the list; sent as If-Match, stripped from the body
}

// Outbound event webhooks: every matching event is POSTed as signed JSON to the
// configured URL (n8n, Node-RED, custom automations).
export function Webhooks() {
  const qc = useQueryClient();
  const { data: hooks } = useQuery({
    queryKey: ["webhooks"],
    queryFn: () => api.get<Webhook[]>("/api/webhooks"),
  });
  const [draft, setDraft] = useState<{ name: string; url: string } | null>(null);
  const [testMsg, setTestMsg] = useState<Record<string, string>>({});

  function invalidate() {
    qc.invalidateQueries({ queryKey: ["webhooks"] });
  }

  async function create() {
    if (!draft || !draft.name.trim() || !draft.url.startsWith("http")) return;
    await api.post("/api/webhooks", {
      name: draft.name.trim(),
      url: draft.url.trim(),
      events: [],
      enabled: true,
    });
    setDraft(null);
    invalidate();
  }
  async function toggle(h: Webhook) {
    await api.put(`/api/webhooks/${h.id}`, { ...h, enabled: !h.enabled }, { ifMatch: h.etag });
    invalidate();
  }
  async function remove(id: string) {
    await api.del(`/api/webhooks/${id}`);
    invalidate();
  }
  async function test(h: Webhook) {
    setTestMsg((m) => ({ ...m, [h.id]: "…" }));
    const r = await api.post<{ ok: boolean; code?: number; error?: string }>(
      `/api/webhooks/${h.id}/test`,
      {},
    );
    setTestMsg((m) => ({ ...m, [h.id]: r.ok ? `OK (${r.code})` : r.error || `HTTP ${r.code}` }));
  }

  return (
    <div className="panel space-y-4">
      <div className="flex items-center gap-2">
        <WebhookIcon className="h-4 w-4 text-fg-muted" />
        <h2 className="text-sm font-medium text-fg">Webhooks</h2>
        <button className="btn-subtle ml-auto h-8" onClick={() => setDraft({ name: "", url: "" })}>
          <Plus className="h-4 w-4" /> Add
        </button>
      </div>
      <p className="text-xs text-fg-muted leading-relaxed">
        POST every event as JSON to any URL. Payloads are signed with{" "}
        <code className="text-fg-faint">X-Aperture-Signature: sha256=…</code> (HMAC of the body).
      </p>

      {draft && (
        <div className="space-y-2 rounded-md border border-line p-3">
          <input
            className="input"
            placeholder="Name"
            value={draft.name}
            onChange={(e) => setDraft({ ...draft, name: e.target.value })}
          />
          <input
            className="input"
            placeholder="https://your-endpoint/hook"
            value={draft.url}
            onChange={(e) => setDraft({ ...draft, url: e.target.value })}
          />
          <div className="flex gap-2">
            <button className="btn-primary h-8" onClick={create}>
              Create
            </button>
            <button className="btn-ghost h-8" onClick={() => setDraft(null)}>
              Cancel
            </button>
          </div>
        </div>
      )}

      <div className="divide-y divide-line">
        {(hooks ?? []).length === 0 && (
          <p className="py-2 text-xs text-fg-faint">No webhooks configured.</p>
        )}
        {(hooks ?? []).map((h) => (
          <div key={h.id} className="flex items-center gap-3 py-2">
            <button
              onClick={() => toggle(h)}
              className={`h-2.5 w-2.5 rounded-full ${h.enabled ? "bg-online" : "bg-fg-faint"}`}
              aria-label={h.enabled ? "Disable" : "Enable"}
            />
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm text-fg">{h.name}</p>
              <p className="truncate font-mono text-[11px] text-fg-faint">{h.url}</p>
            </div>
            {testMsg[h.id] && <span className="text-[11px] text-fg-muted">{testMsg[h.id]}</span>}
            <button className="btn-icon h-8 w-8" onClick={() => test(h)} aria-label="Send test">
              <Send className="h-4 w-4" />
            </button>
            <button className="btn-icon h-8 w-8" onClick={() => remove(h.id)} aria-label="Delete webhook">
              <Trash2 className="h-4 w-4" />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
