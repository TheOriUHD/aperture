import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Webhook as WebhookIcon, Plus, Trash2, Send } from "lucide-react";
import { api } from "../lib/api";
import { Switch } from "./ui/Switch";
import { EmptyState } from "./ui/EmptyState";
import { IntegrationHeader } from "./IntegrationParts";

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

  const list = hooks ?? [];

  return (
    <div>
      <IntegrationHeader
        icon={WebhookIcon}
        title="Webhooks"
        action={
          <button className="btn-outline" onClick={() => setDraft({ name: "", url: "" })}>
            <Plus className="h-4 w-4" /> Add webhook
          </button>
        }
      >
        POST every event as JSON to any URL. Payloads are signed with{" "}
        <code className="text-fg">X-Aperture-Signature: sha256=…</code> (HMAC of the body).
      </IntegrationHeader>

      {draft && (
        <div className="mb-4 mt-2 rounded-lg border border-line p-4">
          <p className="mb-3 text-sm font-semibold text-fg">New webhook</p>
          <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.6fr)]">
            <div>
              <label className="label">Name</label>
              <input
                className="input"
                placeholder="Name"
                value={draft.name}
                onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                autoFocus
              />
            </div>
            <div>
              <label className="label">URL</label>
              <input
                className="input font-mono text-xs"
                placeholder="https://your-endpoint/hook"
                value={draft.url}
                onChange={(e) => setDraft({ ...draft, url: e.target.value })}
              />
            </div>
          </div>
          <div className="mt-4 flex justify-end gap-2">
            <button className="btn-ghost" onClick={() => setDraft(null)}>
              Cancel
            </button>
            <button className="btn-primary" onClick={create}>
              Create
            </button>
          </div>
        </div>
      )}

      {list.length === 0 ? (
        !draft && (
          <EmptyState
            icon={WebhookIcon}
            title="No webhooks configured."
            hint="Add one to send every event to n8n, Node-RED or your own endpoint."
          />
        )
      ) : (
        <div className="mt-2 overflow-x-auto">
          <table className="tbl">
            <thead>
              <tr>
                <th>Name</th>
                <th className="hidden md:table-cell">URL</th>
                <th className="hidden sm:table-cell">Last test</th>
                <th className="w-px">Enabled</th>
                <th className="w-px" aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {list.map((h) => (
                <tr key={h.id}>
                  <td className="max-w-[220px]">
                    <span className="block truncate text-fg">{h.name}</span>
                    <span className="block truncate font-mono text-[11px] text-fg-faint md:hidden">{h.url}</span>
                    {testMsg[h.id] && <span className="block text-[11px] text-fg-muted sm:hidden">{testMsg[h.id]}</span>}
                  </td>
                  <td className="hidden max-w-[360px] md:table-cell">
                    <span className="block truncate font-mono text-xs" title={h.url}>
                      {h.url}
                    </span>
                  </td>
                  <td className="hidden whitespace-nowrap text-xs sm:table-cell">
                    {testMsg[h.id] ? (
                      <span className={testMsg[h.id].startsWith("OK") ? "text-online" : testMsg[h.id] === "…" ? "text-fg-muted" : "text-danger"}>
                        {testMsg[h.id]}
                      </span>
                    ) : (
                      <span className="text-fg-faint">–</span>
                    )}
                  </td>
                  <td>
                    <Switch checked={h.enabled} onChange={() => toggle(h)} label={h.enabled ? "Disable" : "Enable"} />
                  </td>
                  <td className="!px-1">
                    <div className="flex items-center justify-end">
                      <button className="btn-icon" onClick={() => test(h)} aria-label="Send test" title="Send test">
                        <Send className="h-4 w-4" />
                      </button>
                      <button
                        className="btn-icon hover:text-danger"
                        onClick={() => remove(h.id)}
                        aria-label="Delete webhook"
                        title="Delete webhook"
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </div>
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
