import { useEffect, useState } from "react";
import { Bell, BellRing, Plus, Send, Trash2, Pencil, Smartphone } from "lucide-react";
import { Switch } from "./ui/Switch";
import { ChannelModal } from "./ChannelModal";
import { RuleModal } from "./RuleModal";
import {
  useChannels,
  useUpdateChannel,
  useDeleteChannel,
  useTestChannel,
  useRules,
  useSaveRule,
  useDeleteRule,
  useTestRule,
  type Rule,
} from "../hooks/useNotify";
import { pushSupported, pushEnabled, enablePush, disablePush } from "../lib/push";

export function NotificationsSettings() {
  const { data: channels } = useChannels();
  const { data: rules } = useRules();
  const updateCh = useUpdateChannel();
  const delCh = useDeleteChannel();
  const testCh = useTestChannel();
  const saveRule = useSaveRule();
  const delRule = useDeleteRule();
  const testRule = useTestRule();

  const [channelOpen, setChannelOpen] = useState(false);
  const [ruleEdit, setRuleEdit] = useState<Rule | null | undefined>(undefined);
  const [pushOn, setPushOn] = useState(false);
  const [pushBusy, setPushBusy] = useState(false);
  const [flash, setFlash] = useState<{ text: string; ok: boolean } | null>(null);

  useEffect(() => {
    void pushEnabled().then(setPushOn);
  }, []);

  function showFlash(text: string, ok: boolean) {
    setFlash({ text, ok });
    setTimeout(() => setFlash(null), 2500);
  }

  async function togglePush() {
    if (pushBusy) return;
    setPushBusy(true);
    try {
      if (pushOn) {
        await disablePush();
        setPushOn(false);
        showFlash("Push disabled on this device.", true);
      } else {
        await enablePush();
        setPushOn(true);
        showFlash("Push enabled on this device.", true);
      }
    } catch (e) {
      showFlash((e as Error).message, false);
    } finally {
      setPushBusy(false);
    }
  }

  async function test(fn: () => Promise<unknown>) {
    try {
      await fn();
      showFlash("Test notification sent.", true);
    } catch (e) {
      showFlash((e as Error).message || "Test failed", false);
    }
  }

  return (
    <div className="panel">
      <div className="flex items-center justify-between mb-3">
        <h2 className="flex items-center gap-2 text-sm font-medium text-fg">
          <Bell className="h-4 w-4" />
          Notifications
        </h2>
        {flash && (
          <span className={`text-xs ${flash.ok ? "text-online" : "text-rec"}`}>{flash.text}</span>
        )}
      </div>

      {/* push on this device */}
      {pushSupported() && (
        <div className="flex items-center justify-between rounded-lg bg-ink-850 px-3 py-2.5 mb-4">
          <div className="flex items-center gap-2.5">
            <Smartphone className="h-4 w-4 text-brand" />
            <div>
              <div className="text-sm text-fg">Push on this device</div>
              <div className="text-xs text-fg-faint">
                Lock-screen alerts here, even when Aperture is closed.
              </div>
            </div>
          </div>
          <Switch checked={pushOn} onChange={togglePush} label="Push on this device" />
        </div>
      )}

      {/* channels */}
      <div className="mb-4">
        <div className="flex items-center justify-between mb-2">
          <p className="text-xs font-medium text-fg-muted">Channels</p>
          <button className="btn-subtle h-7 text-xs" onClick={() => setChannelOpen(true)}>
            <Plus className="h-3.5 w-3.5" /> Add channel
          </button>
        </div>
        {!channels || channels.length === 0 ? (
          <p className="text-xs text-fg-faint">No channels yet — add Web Push, ntfy, Telegram, email…</p>
        ) : (
          <div className="space-y-1.5">
            {channels.map((ch) => (
              <div key={ch.id} className="flex items-center gap-2 rounded-lg bg-ink-850 px-3 py-2 text-xs">
                <span className="rounded bg-ink-700 px-1.5 py-0.5 font-mono text-fg-muted">{ch.type}</span>
                <span className="flex-1 truncate text-fg">{ch.name}</span>
                <Switch
                  checked={ch.enabled}
                  onChange={(v) => updateCh.mutate({ id: ch.id, name: ch.name, enabled: v })}
                  label="Enabled"
                />
                <button className="btn-icon h-7 w-7" onClick={() => test(() => testCh.mutateAsync(ch.id))} aria-label="Test">
                  <Send className="h-3.5 w-3.5" />
                </button>
                <button className="btn-icon h-7 w-7" onClick={() => delCh.mutate(ch.id)} aria-label="Delete">
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* rules */}
      <div>
        <div className="flex items-center justify-between mb-2">
          <p className="text-xs font-medium text-fg-muted">Rules</p>
          <button
            className="btn-subtle h-7 text-xs"
            onClick={() => setRuleEdit(null)}
            disabled={!channels || channels.length === 0}
          >
            <Plus className="h-3.5 w-3.5" /> Add rule
          </button>
        </div>
        {!rules || rules.length === 0 ? (
          <p className="text-xs text-fg-faint">
            No rules — add one to choose which cameras notify which channels.
          </p>
        ) : (
          <div className="space-y-1.5">
            {rules.map((r) => (
              <div key={r.id} className="flex items-center gap-2 rounded-lg bg-ink-850 px-3 py-2 text-xs">
                <BellRing className={`h-3.5 w-3.5 ${r.enabled ? "text-brand" : "text-fg-faint"}`} />
                <div className="flex-1 min-w-0">
                  <div className="truncate text-fg">{r.name}</div>
                  <div className="text-fg-faint">
                    {r.cameras.length === 0 ? "all cameras" : `${r.cameras.length} cameras`} ·{" "}
                    {r.channels.length} channel{r.channels.length === 1 ? "" : "s"}
                    {r.quietStart >= 0 ? " · quiet hours" : ""}
                  </div>
                </div>
                <Switch
                  checked={r.enabled}
                  onChange={(v) => saveRule.mutate({ ...r, enabled: v })}
                  label="Enabled"
                />
                <button className="btn-icon h-7 w-7" onClick={() => test(() => testRule.mutateAsync(r.id))} aria-label="Test">
                  <Send className="h-3.5 w-3.5" />
                </button>
                <button className="btn-icon h-7 w-7" onClick={() => setRuleEdit(r)} aria-label="Edit">
                  <Pencil className="h-3.5 w-3.5" />
                </button>
                <button className="btn-icon h-7 w-7" onClick={() => delRule.mutate(r.id)} aria-label="Delete">
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {channelOpen && <ChannelModal onClose={() => setChannelOpen(false)} />}
      {ruleEdit !== undefined && (
        <RuleModal rule={ruleEdit} channels={channels ?? []} onClose={() => setRuleEdit(undefined)} />
      )}
    </div>
  );
}
