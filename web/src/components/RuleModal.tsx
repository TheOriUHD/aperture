import { useState } from "react";
import { Check } from "lucide-react";
import { Modal } from "./ui/Modal";
import { Switch } from "./ui/Switch";
import { useSaveRule, type Rule, type Channel } from "../hooks/useNotify";
import { useCameras } from "../hooks/useCameras";

function minToHHMM(m: number): string {
  if (m < 0) m = 0;
  return `${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`;
}
function hhmmToMin(s: string): number {
  const [h, m] = s.split(":").map(Number);
  return (h || 0) * 60 + (m || 0);
}

function Chips({
  options,
  selected,
  onToggle,
}: {
  options: { id: string; label: string }[];
  selected: string[];
  onToggle: (id: string) => void;
}) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {options.map((o) => {
        const on = selected.includes(o.id);
        return (
          <button
            key={o.id}
            onClick={() => onToggle(o.id)}
            type="button"
            aria-pressed={on}
            className={`inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-xs transition-colors ${
              on
                ? "border-brand/60 bg-brand/10 text-brand"
                : "border-line-strong text-fg-muted hover:border-[#3a3e43] hover:text-fg"
            }`}
          >
            {on && <Check className="h-3 w-3" />}
            {o.label}
          </button>
        );
      })}
    </div>
  );
}

export function RuleModal({
  rule,
  channels,
  onClose,
}: {
  rule: Rule | null;
  channels: Channel[];
  onClose: () => void;
}) {
  const save = useSaveRule();
  const { data: cameras } = useCameras();
  const [name, setName] = useState(rule?.name ?? "");
  const [enabled, setEnabled] = useState(rule?.enabled ?? true);
  const [cams, setCams] = useState<string[]>(rule?.cameras ?? []);
  const [chans, setChans] = useState<string[]>(rule?.channels ?? []);
  const [quiet, setQuiet] = useState((rule?.quietStart ?? -1) >= 0);
  const [qStart, setQStart] = useState(minToHHMM(rule?.quietStart ?? 1380));
  const [qEnd, setQEnd] = useState(minToHHMM(rule?.quietEnd ?? 420));
  const [cooldown, setCooldown] = useState(rule?.cooldownSec ?? 120);
  const [timeOfDay, setTimeOfDay] = useState<"" | "day" | "night">(rule?.conditions?.timeOfDay ?? "");
  const [onlyPrecip, setOnlyPrecip] = useState(rule?.conditions?.onlyPrecip ?? false);
  const [objects, setObjects] = useState<string[]>(rule?.conditions?.objects ?? []);
  const [err, setErr] = useState("");

  const toggle = (arr: string[], set: (v: string[]) => void, id: string) =>
    set(arr.includes(id) ? arr.filter((x) => x !== id) : [...arr, id]);

  return (
    <Modal
      open
      onClose={onClose}
      title={rule ? "Edit rule" : "New notification rule"}
      footer={
        <>
          <button className="btn-ghost" onClick={onClose}>
            Cancel
          </button>
          <button
            className="btn-primary"
            disabled={save.isPending}
            onClick={async () => {
              setErr("");
              if (!name.trim()) return setErr("Give the rule a name.");
              if (chans.length === 0) return setErr("Pick at least one channel.");
              try {
                await save.mutateAsync({
                  id: rule?.id,
                  etag: rule?.etag, // sent as If-Match (optimistic concurrency); stripped from the body
                  name: name.trim(),
                  enabled,
                  cameras: cams,
                  eventTypes: [],
                  channels: chans,
                  quietStart: quiet ? hhmmToMin(qStart) : -1,
                  quietEnd: quiet ? hhmmToMin(qEnd) : -1,
                  cooldownSec: cooldown,
                  conditions: { timeOfDay, onlyPrecip, objects },
                });
                onClose();
              } catch (e) {
                setErr((e as Error).message || "Could not save rule");
              }
            }}
          >
            {save.isPending ? "Saving…" : "Save rule"}
          </button>
        </>
      }
    >
      <div className="space-y-5">
        <div>
          <label className="label">Name</label>
          <div className="flex items-center gap-3">
            <input
              className="input flex-1"
              placeholder="e.g. Person at the front door"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <label className="flex shrink-0 items-center gap-2 text-sm text-fg-muted">
              <Switch checked={enabled} onChange={setEnabled} label="Enabled" />
              Enabled
            </label>
          </div>
        </div>

        <div>
          <label className="label">Cameras</label>
          <Chips
            options={(cameras ?? []).map((c) => ({ id: c.id, label: c.name }))}
            selected={cams}
            onToggle={(id) => toggle(cams, setCams, id)}
          />
          <p className="hint mt-1.5">
            {cams.length === 0 ? "All cameras you can access." : `${cams.length} selected.`}
          </p>
        </div>

        <div>
          <label className="label">Notify via</label>
          {channels.length === 0 ? (
            <p className="hint">Add a channel first.</p>
          ) : (
            <Chips
              options={channels.map((c) => ({ id: c.id, label: c.name }))}
              selected={chans}
              onToggle={(id) => toggle(chans, setChans, id)}
            />
          )}
        </div>

        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="label">Cooldown</label>
            <select
              className="input"
              value={cooldown}
              onChange={(e) => setCooldown(Number(e.target.value))}
            >
              <option value={0}>Every event</option>
              <option value={60}>1 minute</option>
              <option value={120}>2 minutes</option>
              <option value={300}>5 minutes</option>
              <option value={900}>15 minutes</option>
            </select>
          </div>
          <div className="flex items-end">
            <label className="flex h-8 cursor-pointer items-center gap-2 text-sm text-fg">
              <input type="checkbox" checked={quiet} onChange={(e) => setQuiet(e.target.checked)} />
              Quiet hours
            </label>
          </div>
        </div>

        {quiet && (
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="label">From</label>
              <input
                className="input"
                type="time"
                value={qStart}
                onChange={(e) => setQStart(e.target.value)}
              />
            </div>
            <div>
              <label className="label">To</label>
              <input className="input" type="time" value={qEnd} onChange={(e) => setQEnd(e.target.value)} />
            </div>
          </div>
        )}

        <div className="border-t border-line pt-5">
          <p className="mb-3 text-[13px] font-semibold text-fg">Only notify when…</p>
          <div className="grid grid-cols-2 gap-3">
            <select
              className="input"
              value={timeOfDay}
              onChange={(e) => setTimeOfDay(e.target.value as "" | "day" | "night")}
              aria-label="Time of day"
            >
              <option value="">Any time of day</option>
              <option value="day">Daytime only</option>
              <option value="night">Nighttime only</option>
            </select>
            <label className="flex h-8 cursor-pointer items-center gap-2 text-sm text-fg">
              <input type="checkbox" checked={onlyPrecip} onChange={(e) => setOnlyPrecip(e.target.checked)} />
              Only when raining
            </label>
          </div>
          <p className="hint mt-1.5">
            Time of day and rain use your site’s sun times and recorded weather — ignored until a location is set
            in Settings → Environment.
          </p>
          <input
            className="input mt-4"
            value={objects.join(", ")}
            onChange={(e) => setObjects(e.target.value.split(",").map((s) => s.trim()).filter(Boolean))}
            placeholder="Only these objects, e.g. person, car (optional)"
            aria-label="Object filter"
          />
          <p className="hint mt-1.5">
            Object filter: notify only when the detector tagged one of these labels (comma-separated). Needs object
            detection enabled.
          </p>
        </div>

        {err && <p className="text-xs text-danger">{err}</p>}
      </div>
    </Modal>
  );
}
