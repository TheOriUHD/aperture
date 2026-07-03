import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Workflow, Plus, Trash2, Play, ChevronDown } from "lucide-react";
import { api } from "../lib/api";
import { useCameras } from "../hooks/useCameras";

interface Trigger {
  eventTypes?: string[];
  objects?: string[];
  cameras?: string[];
}
interface Conditions {
  timeOfDay?: string;
  onlyPrecip?: boolean;
}
interface Action {
  type: "notify" | "webhook" | "mqtt" | "record_mode";
  channels?: string[];
  url?: string;
  topic?: string;
  payload?: string;
  camera?: string;
  recordMode?: string;
}
interface Automation {
  id: string;
  name: string;
  enabled: boolean;
  trigger: Trigger;
  conditions: Conditions;
  actions: Action[];
  cooldownMs: number;
}
interface Channel {
  id: string;
  name: string;
}

const OBJECTS = ["person", "car", "truck", "dog", "cat", "bicycle"];
const MODES = ["never", "motion", "continuous"];

const blank = (): Automation => ({
  id: "",
  name: "",
  enabled: true,
  trigger: {},
  conditions: {},
  actions: [{ type: "notify", channels: [] }],
  cooldownMs: 0,
});

// The local when-this-then-that builder: a trigger (event + object + camera)
// filtered by conditions (time of day, weather) firing actions (notify, webhook,
// MQTT publish, set a camera's record mode).
export function Automations() {
  const qc = useQueryClient();
  const { data: cameras } = useCameras();
  const { data: channels } = useQuery({
    queryKey: ["notify-channels"],
    queryFn: () => api.get<Channel[]>("/api/notify/channels"),
  });
  const { data: autos } = useQuery({
    queryKey: ["automations"],
    queryFn: () => api.get<Automation[]>("/api/automations"),
  });
  const [editing, setEditing] = useState<Automation | null>(null);

  function invalidate() {
    qc.invalidateQueries({ queryKey: ["automations"] });
  }
  async function save(a: Automation) {
    const body = { ...a, cooldownMs: a.cooldownMs || 0 };
    if (a.id) await api.put(`/api/automations/${a.id}`, body);
    else await api.post("/api/automations", body);
    setEditing(null);
    invalidate();
  }
  async function toggle(a: Automation) {
    await api.put(`/api/automations/${a.id}`, { ...a, enabled: !a.enabled });
    invalidate();
  }
  async function remove(id: string) {
    await api.del(`/api/automations/${id}`);
    invalidate();
  }
  async function test(id: string) {
    await api.post(`/api/automations/${id}/test`, {});
  }

  return (
    <div className="panel space-y-4">
      <div className="flex items-center gap-2">
        <Workflow className="h-4 w-4 text-fg-muted" />
        <h2 className="text-sm font-medium text-fg">Automations</h2>
        <button className="btn-subtle ml-auto h-8" onClick={() => setEditing(blank())}>
          <Plus className="h-4 w-4" /> New
        </button>
      </div>
      <p className="text-xs text-fg-muted leading-relaxed">
        When an event matches, run actions — notify, fire a webhook, publish MQTT, or change a camera's
        record mode. e.g. <em>“person on Front Door at night → set Back camera to continuous + notify”.</em>
      </p>

      {editing && (
        <Editor
          value={editing}
          cameras={cameras ?? []}
          channels={channels ?? []}
          onCancel={() => setEditing(null)}
          onSave={save}
        />
      )}

      <div className="divide-y divide-line">
        {(autos ?? []).length === 0 && !editing && (
          <p className="py-2 text-xs text-fg-faint">No automations yet.</p>
        )}
        {(autos ?? []).map((a) => (
          <div key={a.id} className="flex items-center gap-3 py-2">
            <button
              onClick={() => toggle(a)}
              className={`h-2.5 w-2.5 rounded-full ${a.enabled ? "bg-online" : "bg-fg-faint"}`}
              aria-label={a.enabled ? "Disable" : "Enable"}
            />
            <button className="min-w-0 flex-1 text-left" onClick={() => setEditing(a)}>
              <p className="truncate text-sm text-fg">{a.name}</p>
              <p className="truncate text-[11px] text-fg-faint">
                {(a.actions ?? []).map((x) => x.type).join(", ") || "no actions"}
              </p>
            </button>
            <button className="btn-icon h-8 w-8" onClick={() => test(a.id)} aria-label="Run now">
              <Play className="h-4 w-4" />
            </button>
            <button className="btn-icon h-8 w-8" onClick={() => remove(a.id)} aria-label="Delete">
              <Trash2 className="h-4 w-4" />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}

function Editor({
  value,
  cameras,
  channels,
  onCancel,
  onSave,
}: {
  value: Automation;
  cameras: { id: string; name: string }[];
  channels: Channel[];
  onCancel: () => void;
  onSave: (a: Automation) => void;
}) {
  const [a, setA] = useState<Automation>(value);
  const set = (patch: Partial<Automation>) => setA((c) => ({ ...c, ...patch }));
  const setTrigger = (patch: Partial<Trigger>) => set({ trigger: { ...a.trigger, ...patch } });
  const setCond = (patch: Partial<Conditions>) => set({ conditions: { ...a.conditions, ...patch } });

  function setAction(i: number, patch: Partial<Action>) {
    const actions = a.actions.slice();
    actions[i] = { ...actions[i], ...patch };
    set({ actions });
  }
  function addAction() {
    set({ actions: [...a.actions, { type: "notify", channels: [] }] });
  }
  function removeAction(i: number) {
    set({ actions: a.actions.filter((_, j) => j !== i) });
  }
  function toggleIn(list: string[] | undefined, v: string): string[] {
    const s = list ?? [];
    return s.includes(v) ? s.filter((x) => x !== v) : [...s, v];
  }

  return (
    <div className="space-y-3 rounded-md border border-line-strong p-3">
      <input
        className="input"
        placeholder="Automation name"
        value={a.name}
        onChange={(e) => set({ name: e.target.value })}
      />

      {/* Trigger */}
      <div>
        <p className="label">When — objects (any if none)</p>
        <div className="flex flex-wrap gap-1.5">
          {OBJECTS.map((o) => (
            <Chip key={o} on={(a.trigger.objects ?? []).includes(o)} onClick={() => setTrigger({ objects: toggleIn(a.trigger.objects, o) })}>
              {o}
            </Chip>
          ))}
        </div>
      </div>
      <div>
        <p className="label">On cameras (any if none)</p>
        <div className="flex flex-wrap gap-1.5">
          {cameras.map((c) => (
            <Chip key={c.id} on={(a.trigger.cameras ?? []).includes(c.id)} onClick={() => setTrigger({ cameras: toggleIn(a.trigger.cameras, c.id) })}>
              {c.name}
            </Chip>
          ))}
        </div>
      </div>

      {/* Conditions */}
      <div className="flex flex-wrap items-center gap-3">
        <label className="text-xs text-fg-muted">Time</label>
        <select className="input h-8 w-28" value={a.conditions.timeOfDay ?? ""} onChange={(e) => setCond({ timeOfDay: e.target.value })}>
          <option value="">Any</option>
          <option value="day">Day</option>
          <option value="night">Night</option>
        </select>
        <label className="flex items-center gap-1.5 text-xs text-fg-muted">
          <input type="checkbox" checked={!!a.conditions.onlyPrecip} onChange={(e) => setCond({ onlyPrecip: e.target.checked })} />
          Only when raining
        </label>
        <label className="ml-auto flex items-center gap-1.5 text-xs text-fg-muted">
          Cooldown
          <input
            type="number"
            className="input h-8 w-20"
            value={Math.round((a.cooldownMs || 0) / 1000)}
            onChange={(e) => set({ cooldownMs: Number(e.target.value) * 1000 })}
          />
          s
        </label>
      </div>

      {/* Actions */}
      <div className="space-y-2">
        <p className="label">Then</p>
        {a.actions.map((act, i) => (
          <div key={i} className="rounded-md border border-line p-2 space-y-2">
            <div className="flex items-center gap-2">
              <div className="relative">
                <select
                  className="input h-8 w-36 appearance-none pr-7"
                  value={act.type}
                  onChange={(e) => setAction(i, { type: e.target.value as Action["type"] })}
                >
                  <option value="notify">Notify</option>
                  <option value="webhook">Webhook</option>
                  <option value="mqtt">MQTT publish</option>
                  <option value="record_mode">Set record mode</option>
                </select>
                <ChevronDown className="pointer-events-none absolute right-2 top-2 h-4 w-4 text-fg-faint" />
              </div>
              <button className="btn-icon ml-auto h-7 w-7" onClick={() => removeAction(i)} aria-label="Remove action">
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            </div>

            {act.type === "notify" && (
              <div className="flex flex-wrap gap-1.5">
                {channels.length === 0 && <span className="text-[11px] text-fg-faint">No channels configured.</span>}
                {channels.map((ch) => (
                  <Chip key={ch.id} on={(act.channels ?? []).includes(ch.id)} onClick={() => setAction(i, { channels: toggleIn(act.channels, ch.id) })}>
                    {ch.name}
                  </Chip>
                ))}
              </div>
            )}
            {act.type === "webhook" && (
              <input className="input" placeholder="https://endpoint" value={act.url ?? ""} onChange={(e) => setAction(i, { url: e.target.value })} />
            )}
            {act.type === "mqtt" && (
              <div className="grid grid-cols-2 gap-2">
                <input className="input" placeholder="topic" value={act.topic ?? ""} onChange={(e) => setAction(i, { topic: e.target.value })} />
                <input className="input" placeholder="payload" value={act.payload ?? ""} onChange={(e) => setAction(i, { payload: e.target.value })} />
              </div>
            )}
            {act.type === "record_mode" && (
              <div className="grid grid-cols-2 gap-2">
                <select className="input" value={act.camera ?? ""} onChange={(e) => setAction(i, { camera: e.target.value })}>
                  <option value="">Camera…</option>
                  {cameras.map((c) => (
                    <option key={c.id} value={c.id}>{c.name}</option>
                  ))}
                </select>
                <select className="input" value={act.recordMode ?? ""} onChange={(e) => setAction(i, { recordMode: e.target.value })}>
                  <option value="">Mode…</option>
                  {MODES.map((m) => (
                    <option key={m} value={m} className="capitalize">{m}</option>
                  ))}
                </select>
              </div>
            )}
          </div>
        ))}
        <button className="btn-ghost h-8" onClick={addAction}>
          <Plus className="h-4 w-4" /> Add action
        </button>
      </div>

      <div className="flex gap-2 pt-1">
        <button className="btn-primary h-9" onClick={() => onSave(a)} disabled={!a.name.trim()}>
          Save
        </button>
        <button className="btn-ghost h-9" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </div>
  );
}

function Chip({ on, onClick, children }: { on: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={`rounded-full border px-2.5 py-1 text-xs capitalize transition-colors ${
        on ? "border-brand bg-brand/15 text-fg" : "border-line text-fg-muted hover:text-fg"
      }`}
    >
      {children}
    </button>
  );
}
