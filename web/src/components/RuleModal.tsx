import { useState } from "react";
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
            className={`h-7 px-2.5 rounded-md text-xs border ${
              on
                ? "bg-brand/15 text-brand border-brand/40"
                : "bg-ink-800 text-fg-muted border-line-strong hover:bg-ink-700"
            }`}
          >
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
          <button className="btn-subtle" onClick={onClose}>
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
                  name: name.trim(),
                  enabled,
                  cameras: cams,
                  eventTypes: [],
                  channels: chans,
                  quietStart: quiet ? hhmmToMin(qStart) : -1,
                  quietEnd: quiet ? hhmmToMin(qEnd) : -1,
                  cooldownSec: cooldown,
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
      <div className="space-y-4">
        <div className="flex items-center gap-3">
          <input
            className="input flex-1"
            placeholder="e.g. Person at the front door"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <Switch checked={enabled} onChange={setEnabled} label="Enabled" />
        </div>

        <div>
          <label className="label">Cameras</label>
          <Chips
            options={(cameras ?? []).map((c) => ({ id: c.id, label: c.name }))}
            selected={cams}
            onToggle={(id) => toggle(cams, setCams, id)}
          />
          <p className="text-xs text-fg-faint mt-1.5">
            {cams.length === 0 ? "All cameras you can access." : `${cams.length} selected.`}
          </p>
        </div>

        <div>
          <label className="label">Notify via</label>
          {channels.length === 0 ? (
            <p className="text-xs text-fg-faint">Add a channel first.</p>
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
          <div className="flex items-end pb-0.5">
            <label className="flex items-center gap-2 text-sm text-fg">
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

        {err && <p className="text-xs text-rec">{err}</p>}
      </div>
    </Modal>
  );
}
