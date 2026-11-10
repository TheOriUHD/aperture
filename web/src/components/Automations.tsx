import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import {
  Bell,
  ChevronDown,
  ListFilter,
  Play,
  Plus,
  Radar,
  RadioTower,
  Trash2,
  Video,
  Webhook,
  Workflow,
  type LucideIcon,
} from "lucide-react";
import { api } from "../lib/api";
import { fmtRelative } from "../lib/format";
import { useCameras } from "../hooks/useCameras";
import { Modal } from "./ui/Modal";
import { Switch } from "./ui/Switch";
import { Spinner } from "./ui/Spinner";
import { EmptyState } from "./ui/EmptyState";

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
  lastFired?: number; // set by the engine each time the automation fires (ms); display only
  etag?: string; // per-item concurrency tag from the list; sent as If-Match, stripped from the body
}
interface Channel {
  id: string;
  name: string;
}

const OBJECTS = ["person", "car", "truck", "dog", "cat", "bicycle", "package"];
const MODES = ["never", "motion", "continuous", "scheduled"];

const blank = (): Automation => ({
  id: "",
  name: "",
  enabled: true,
  trigger: {},
  conditions: {},
  actions: [{ type: "notify", channels: [] }],
  cooldownMs: 0,
});

// Labels for an action type, as the editor's action picker names them.
const ACTION_LABEL: Record<Action["type"], string> = {
  notify: "Notify",
  webhook: "Webhook",
  mqtt: "MQTT publish",
  record_mode: "Set record mode",
};

const cap = (s: string) => (s ? s[0].toUpperCase() + s.slice(1) : s);

// triggerSummary is the table's one-line "what fires this": the objects (or raw
// event types) it listens for, plus any time-of-day / weather condition.
function triggerSummary(a: Automation): string {
  const objs = a.trigger?.objects ?? [];
  const types = a.trigger?.eventTypes ?? [];
  const what = objs.length ? objs.map(cap).join(", ") : types.length ? types.map(cap).join(", ") : "Any object";
  const cond = [
    a.conditions?.timeOfDay ? cap(a.conditions.timeOfDay) : "",
    a.conditions?.onlyPrecip ? "Raining" : "",
  ].filter(Boolean);
  return [what, ...cond].join(" · ");
}

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
  const { data: autos, isLoading } = useQuery({
    queryKey: ["automations"],
    queryFn: () => api.get<Automation[]>("/api/automations"),
  });
  const [editing, setEditing] = useState<Automation | null>(null);

  function invalidate() {
    qc.invalidateQueries({ queryKey: ["automations"] });
  }
  async function save(a: Automation) {
    // etag rides the If-Match header (optimistic concurrency); stripServerEnvelope
    // drops it from the spread body so DisallowUnknownFields won't 400. A freshly
    // built automation has no etag → create posts without If-Match.
    const body = { ...a, cooldownMs: a.cooldownMs || 0 };
    if (a.id) await api.put(`/api/automations/${a.id}`, body, { ifMatch: a.etag });
    else await api.post("/api/automations", body);
    setEditing(null);
    invalidate();
  }
  async function toggle(a: Automation) {
    await api.put(`/api/automations/${a.id}`, { ...a, enabled: !a.enabled }, { ifMatch: a.etag });
    invalidate();
  }
  async function remove(id: string) {
    await api.del(`/api/automations/${id}`);
    invalidate();
  }
  async function test(id: string) {
    await api.post(`/api/automations/${id}/test`, {});
  }

  const list = autos ?? [];
  const camName = (id: string) => cameras?.find((c) => c.id === id)?.name ?? id;

  return (
    <div className="space-y-5">
      <Pipeline />

      <div>
        <div className="mb-2 flex items-center gap-3">
          <h2 className="text-sm font-semibold text-fg">
            All automations
            {list.length > 0 && <span className="ml-2 font-normal tabular-nums text-fg-faint">{list.length}</span>}
          </h2>
          <button className="btn-outline ml-auto" onClick={() => setEditing(blank())}>
            <Plus className="h-4 w-4" /> Create automation
          </button>
        </div>

        {isLoading ? (
          <div className="grid place-items-center py-16">
            <Spinner label="Loading automations" />
          </div>
        ) : list.length === 0 ? (
          <div className="border-t border-line">
            <EmptyState
              icon={Workflow}
              title="No automations yet."
              hint="Create one to turn matching events into notifications, webhooks, MQTT messages or record-mode changes."
            />
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="tbl tbl-hover">
              <thead>
                <tr>
                  <th className="w-full sm:w-auto">Name</th>
                  <th className="hidden md:table-cell">Trigger</th>
                  <th className="hidden sm:table-cell">Actions</th>
                  <th className="hidden lg:table-cell">Cameras</th>
                  <th className="hidden sm:table-cell">Last triggered</th>
                  <th className="w-px">Enabled</th>
                  <th className="w-px" aria-label="Row actions" />
                </tr>
              </thead>
              <tbody>
                {list.map((a) => {
                  const actions = (a.actions ?? []).map((x) => ACTION_LABEL[x.type] ?? x.type).join(", ") || "No actions";
                  const cams = a.trigger?.cameras ?? [];
                  const camText = cams.length ? cams.map(camName).join(", ") : "All cameras";
                  return (
                    <tr key={a.id} onClick={() => setEditing(a)} className={a.enabled ? undefined : "opacity-60"}>
                      <td className="max-w-0 sm:max-w-[260px]">
                        <span className="block truncate font-medium text-fg">{a.name}</span>
                        <span className="block truncate text-xs text-fg-faint sm:hidden">{actions}</span>
                      </td>
                      <td className="hidden max-w-[240px] md:table-cell">
                        <span className="block truncate" title={triggerSummary(a)}>
                          {triggerSummary(a)}
                        </span>
                      </td>
                      <td className="hidden max-w-[220px] sm:table-cell">
                        <span className="block truncate" title={actions}>
                          {actions}
                        </span>
                      </td>
                      <td className="hidden max-w-[220px] lg:table-cell">
                        <span className={cams.length ? "block truncate" : "block truncate text-fg-faint"} title={camText}>
                          {camText}
                        </span>
                      </td>
                      <td className="hidden whitespace-nowrap sm:table-cell">
                        {a.lastFired ? (
                          <span title={new Date(a.lastFired).toLocaleString()}>{fmtRelative(a.lastFired)}</span>
                        ) : (
                          <span className="text-fg-faint">–</span>
                        )}
                      </td>
                      <td onClick={(e) => e.stopPropagation()}>
                        <Switch checked={a.enabled} onChange={() => toggle(a)} label={a.enabled ? "Disable" : "Enable"} />
                      </td>
                      <td className="!px-1" onClick={(e) => e.stopPropagation()}>
                        <div className="flex items-center justify-end">
                          <button className="btn-icon" onClick={() => test(a.id)} aria-label="Run now" title="Run now">
                            <Play className="h-4 w-4" />
                          </button>
                          <button className="btn-icon hover:text-danger" onClick={() => remove(a.id)} aria-label="Delete" title="Delete">
                            <Trash2 className="h-4 w-4" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {editing && (
        <Editor
          value={editing}
          cameras={cameras ?? []}
          channels={channels ?? []}
          onCancel={() => setEditing(null)}
          onSave={save}
        />
      )}
    </div>
  );
}

// Pipeline is the explainer on top of the page: what an automation is made of,
// drawn as the trigger → conditions → actions chain the editor builds.
function Pipeline() {
  return (
    <section className="panel flex flex-col gap-5 lg:flex-row lg:items-center lg:gap-8">
      <div className="hidden shrink-0 items-start md:flex">
        <div className="flex items-start pt-[13px]">
          <Node icon={Radar} title="Trigger" sub="Person, vehicle, etc." wide />
          <Connector />
          <Node icon={ListFilter} title="Conditions" sub="Time, weather" wide />
          <Connector edge />
        </div>
        <div className="rounded-lg border border-brand/25 bg-brand-ink/50 px-2 pb-2 pt-3">
          <div className="flex items-start">
            <Node icon={Bell} label="Notify" />
            <Connector short />
            <Node icon={Webhook} label="Webhook" />
            <Connector short />
            <Node icon={RadioTower} label="MQTT" />
            <Connector short />
            <Node icon={Video} label="Record mode" />
          </div>
          <p className="mt-1 text-center text-xs font-semibold text-fg">Actions</p>
        </div>
      </div>
      <p className="max-w-[560px] text-sm leading-relaxed text-fg-muted">
        <span className="font-semibold text-fg">Automations</span> run actions when an event matches — notify,
        fire a webhook, publish MQTT, or change a camera's record mode. e.g.{" "}
        <em>“person on Front Door at night → set Back camera to continuous + notify”.</em>
      </p>
    </section>
  );
}

function Node({
  icon: Icon,
  title,
  sub,
  label,
  wide,
}: {
  icon: LucideIcon;
  title?: string;
  sub?: string;
  label?: string;
  wide?: boolean;
}) {
  return (
    <div className={clsx("flex flex-col items-center text-center", wide ? "w-[116px]" : "w-[84px]")}>
      <span className="grid h-7 w-7 place-items-center rounded-full border border-brand/40 bg-ink-900">
        <Icon className="h-3.5 w-3.5 text-brand" />
      </span>
      {label && <span className="mt-1.5 text-[11px] text-fg-muted">{label}</span>}
      {title && <span className="mt-1.5 text-xs font-semibold text-fg">{title}</span>}
      {sub && <span className="mt-0.5 text-[11px] leading-snug text-fg-faint">{sub}</span>}
    </div>
  );
}

// Connector is the hairline between two nodes, drawn at the height of their icons.
function Connector({ short, edge }: { short?: boolean; edge?: boolean }) {
  return (
    <span
      className={clsx(
        "mt-3.5 h-px shrink-0 bg-brand/40",
        edge ? "-ml-9 w-14" : short ? "-mx-5 w-10" : "-mx-9 w-20",
      )}
    />
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
    <Modal
      open
      onClose={onCancel}
      title={value.id ? "Edit automation" : "New automation"}
      size="lg"
      footer={
        <>
          <button className="btn-ghost" onClick={onCancel}>
            Cancel
          </button>
          <button className="btn-primary" onClick={() => onSave(a)} disabled={!a.name.trim()}>
            Save
          </button>
        </>
      }
    >
      <div className="-my-1 divide-y divide-line">
        <div className="pb-5">
          <label className="label">Name</label>
          <input
            className="input"
            placeholder="Automation name"
            value={a.name}
            onChange={(e) => set({ name: e.target.value })}
            autoFocus={!value.id}
          />
        </div>

        {/* Trigger */}
        <div className="space-y-4 py-5">
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
        </div>

        {/* Conditions */}
        <div className="flex flex-wrap items-end gap-x-6 gap-y-3 py-5">
          <div>
            <label className="label">Time</label>
            <select className="input w-32" value={a.conditions.timeOfDay ?? ""} onChange={(e) => setCond({ timeOfDay: e.target.value })}>
              <option value="">Any</option>
              <option value="day">Day</option>
              <option value="night">Night</option>
            </select>
          </div>
          <label className="flex h-8 cursor-pointer items-center gap-2 text-sm text-fg/85">
            <input type="checkbox" checked={!!a.conditions.onlyPrecip} onChange={(e) => setCond({ onlyPrecip: e.target.checked })} />
            Only when raining
          </label>
          <div className="sm:ml-auto">
            <label className="label">Cooldown</label>
            <div className="flex items-center gap-2 text-sm text-fg-muted">
              <input
                type="number"
                className="input w-24"
                value={Math.round((a.cooldownMs || 0) / 1000)}
                onChange={(e) => set({ cooldownMs: Number(e.target.value) * 1000 })}
              />
              s
            </div>
          </div>
        </div>

        {/* Actions */}
        <div className="space-y-2 pt-5">
          <p className="label">Then</p>
          {a.actions.map((act, i) => (
            <div key={i} className="space-y-2.5 rounded-lg border border-line bg-ink-850 p-3">
              <div className="flex items-center gap-2">
                <div className="relative">
                  <select
                    className="input w-44 appearance-none pr-7"
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
                <button className="btn-icon ml-auto hover:text-danger" onClick={() => removeAction(i)} aria-label="Remove action" title="Remove action">
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>

              {act.type === "notify" && (
                <div className="flex flex-wrap gap-1.5">
                  {channels.length === 0 && <span className="text-xs text-fg-faint">No channels configured.</span>}
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
          <button className="btn-ghost" onClick={addAction}>
            <Plus className="h-4 w-4" /> Add action
          </button>
        </div>
      </div>
    </Modal>
  );
}

function Chip({ on, onClick, children }: { on: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={on}
      className={clsx(
        "inline-flex h-7 items-center rounded-md border px-2.5 text-xs capitalize transition-colors",
        on ? "border-brand/60 bg-brand/10 text-brand" : "border-line-strong text-fg-muted hover:border-ink-600 hover:text-fg",
      )}
    >
      {children}
    </button>
  );
}
