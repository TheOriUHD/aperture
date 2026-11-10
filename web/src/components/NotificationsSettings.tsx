import { useEffect, useState, type ReactNode } from "react";
import { BellRing, Plus, Send, Trash2, Pencil, type LucideIcon } from "lucide-react";
import { Switch } from "./ui/Switch";
import { SettingsHeading, SettingsRow, StatusDot } from "./ui/Layout";
import { Skeleton } from "./ui/Skeleton";
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
import {
  deviceNotifySupported,
  deviceNotifyEnabled,
  enableDeviceNotify,
  disableDeviceNotify,
} from "../lib/deviceNotify";

export function NotificationsSettings() {
  const { data: channels, isLoading: channelsLoading } = useChannels();
  const { data: rules, isLoading: rulesLoading } = useRules();
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
  const [notifyOn, setNotifyOn] = useState(deviceNotifyEnabled());
  const [flash, setFlash] = useState<{ text: string; ok: boolean } | null>(null);

  useEffect(() => {
    void pushEnabled().then(setPushOn);
  }, []);

  async function toggleNotify() {
    try {
      if (notifyOn) {
        disableDeviceNotify();
        setNotifyOn(false);
        showFlash("On-device notifications off.", true);
      } else {
        await enableDeviceNotify();
        setNotifyOn(true);
        showFlash("On-device notifications on.", true);
      }
    } catch (e) {
      showFlash((e as Error).message, false);
    }
  }

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

  const flush = "[&_td:first-child]:pl-0 [&_td:last-child]:pr-0 [&_th:first-child]:pl-0 [&_th:last-child]:pr-0";

  return (
    <>
      {(deviceNotifySupported() || pushSupported()) && (
        <>
          <SettingsHeading>This device</SettingsHeading>
          <div className="divide-y divide-line">
            {/* notify on this device (SSE + Notification API — no external push service) */}
            {deviceNotifySupported() && (
              <SettingsRow
                label="Notify on this device"
                hint="Alerts while Aperture is open in a tab or installed — no Google/Apple push service needed."
              >
                <Switch checked={notifyOn} onChange={toggleNotify} label="Notify on this device" />
              </SettingsRow>
            )}

            {/* optional Web Push — also alerts when fully closed (needs the browser's push service) */}
            {pushSupported() && (
              <SettingsRow
                label="Also alert when closed (Web Push)"
                hint={
                  <>
                    Lock-screen alerts even when Aperture isn’t open. Needs your browser’s push service (Brave: enable
                    “Use Google services for push messaging”).
                  </>
                }
              >
                <Switch checked={pushOn} onChange={togglePush} label="Web Push" />
              </SettingsRow>
            )}
          </div>
        </>
      )}

      {/* channels */}
      <ListHeader
        title="Channels"
        description="Where alerts are delivered: Web Push, ntfy, Telegram, email…"
        action={
          <button className="btn-outline" onClick={() => setChannelOpen(true)}>
            <Plus className="h-4 w-4" /> Add channel
          </button>
        }
      />
      {channelsLoading ? (
        <ListSkeleton />
      ) : !channels || channels.length === 0 ? (
        <EmptyRow icon={Send} text="No channels yet — add Web Push, ntfy, Telegram, email…" />
      ) : (
        <div className="overflow-x-auto">
          <table className={`tbl table-fixed ${flush}`}>
            <thead>
              <tr>
                <th>Name</th>
                <th className="w-28 sm:w-44">Type</th>
                <th className="w-[72px]">Enabled</th>
                <th className="w-[72px]" aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {channels.map((ch) => (
                <tr key={ch.id} className="hover:bg-white/[0.02]">
                  <td>
                    <span className={`block truncate ${ch.enabled ? "text-fg" : "text-fg-muted"}`}>{ch.name}</span>
                  </td>
                  <td>
                    <span className="pill-gray">{ch.type}</span>
                  </td>
                  <td>
                    <Switch
                      checked={ch.enabled}
                      onChange={(v) => updateCh.mutate({ id: ch.id, name: ch.name, enabled: v, etag: ch.etag })}
                      label="Enabled"
                    />
                  </td>
                  <td>
                    <div className="flex justify-end">
                      <button
                        className="btn-icon"
                        onClick={() => test(() => testCh.mutateAsync(ch.id))}
                        aria-label="Test"
                        title="Send a test notification"
                      >
                        <Send className="h-4 w-4" />
                      </button>
                      <button
                        className="btn-icon hover:text-danger"
                        onClick={() => delCh.mutate(ch.id)}
                        aria-label="Delete"
                        title="Delete"
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

      {/* rules */}
      <ListHeader
        title="Rules"
        description="Which cameras notify which channels, and when."
        action={
          <button
            className="btn-outline"
            onClick={() => setRuleEdit(null)}
            disabled={!channels || channels.length === 0}
          >
            <Plus className="h-4 w-4" /> Add rule
          </button>
        }
      />
      {rulesLoading ? (
        <ListSkeleton />
      ) : !rules || rules.length === 0 ? (
        <EmptyRow icon={BellRing} text="No rules — add one to choose which cameras notify which channels." />
      ) : (
        <div className="overflow-x-auto">
          <table className={`tbl table-fixed ${flush}`}>
            <thead>
              <tr>
                <th>Name</th>
                <th className="hidden w-32 sm:table-cell">Cameras</th>
                <th className="hidden w-32 sm:table-cell">Channels</th>
                <th className="hidden w-32 md:table-cell">Quiet hours</th>
                <th className="w-[72px]">Enabled</th>
                <th className="w-[104px]" aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {rules.map((r) => (
                <tr key={r.id} className="hover:bg-white/[0.02]">
                  <td>
                    <div className="flex min-w-0 items-center gap-2.5">
                      <BellRing className={`h-4 w-4 shrink-0 ${r.enabled ? "text-brand" : "text-fg-faint"}`} />
                      <div className="min-w-0">
                        <div className={`truncate ${r.enabled ? "text-fg" : "text-fg-muted"}`}>{r.name}</div>
                        {/* Compact summary where the detail columns are hidden. */}
                        <div className="truncate text-xs text-fg-faint sm:hidden">
                          {r.cameras.length === 0 ? "all cameras" : `${r.cameras.length} camera${r.cameras.length === 1 ? "" : "s"}`} ·{" "}
                          {r.channels.length} channel{r.channels.length === 1 ? "" : "s"}
                          {r.quietStart >= 0 ? " · quiet hours" : ""}
                        </div>
                      </div>
                    </div>
                  </td>
                  <td className="hidden whitespace-nowrap sm:table-cell">
                    {r.cameras.length === 0 ? "All cameras" : `${r.cameras.length} camera${r.cameras.length === 1 ? "" : "s"}`}
                  </td>
                  <td className="hidden whitespace-nowrap sm:table-cell">
                    {r.channels.length} channel{r.channels.length === 1 ? "" : "s"}
                  </td>
                  <td className="hidden whitespace-nowrap md:table-cell">
                    {r.quietStart >= 0 ? (
                      <span className="tabular-nums">
                        {hhmm(r.quietStart)}–{hhmm(r.quietEnd)}
                      </span>
                    ) : (
                      <span className="text-fg-faint">—</span>
                    )}
                  </td>
                  <td>
                    <Switch
                      checked={r.enabled}
                      onChange={(v) => saveRule.mutate({ ...r, enabled: v })}
                      label="Enabled"
                    />
                  </td>
                  <td>
                    <div className="flex justify-end">
                      <button
                        className="btn-icon"
                        onClick={() => test(() => testRule.mutateAsync(r.id))}
                        aria-label="Test"
                        title="Send a test notification"
                      >
                        <Send className="h-4 w-4" />
                      </button>
                      <button className="btn-icon" onClick={() => setRuleEdit(r)} aria-label="Edit" title="Edit">
                        <Pencil className="h-4 w-4" />
                      </button>
                      <button
                        className="btn-icon hover:text-danger"
                        onClick={() => delRule.mutate(r.id)}
                        aria-label="Delete"
                        title="Delete"
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

      {channelOpen && <ChannelModal onClose={() => setChannelOpen(false)} />}
      {ruleEdit !== undefined && (
        <RuleModal rule={ruleEdit} channels={channels ?? []} onClose={() => setRuleEdit(undefined)} />
      )}

      {/* Result of the last toggle/test: a small toast, so it is seen wherever the
          button that triggered it sits on the page. */}
      {flash && (
        <div
          role="status"
          className="card fixed bottom-24 left-1/2 z-50 flex max-w-[90vw] -translate-x-1/2 animate-fade-in items-center gap-2.5 px-4 py-2.5 text-sm text-fg shadow-pop lg:bottom-6"
        >
          <StatusDot tone={flash.ok ? "ok" : "bad"} />
          {flash.text}
        </div>
      )}
    </>
  );
}

// Minutes after midnight → "23:00", for the quiet-hours column.
function hhmm(m: number): string {
  return `${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`;
}

// A list's heading with its create action on the right.
function ListHeader({ title, description, action }: { title: string; description?: string; action?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2 pt-6 first:pt-0">
      <div className="min-w-0">
        <h3 className="text-sm font-semibold text-fg">{title}</h3>
        {description && <p className="mt-1 text-xs leading-relaxed text-fg-faint">{description}</p>}
      </div>
      {action}
    </div>
  );
}

function ListSkeleton() {
  return (
    <div className="space-y-2">
      <Skeleton className="h-9" />
      <Skeleton className="h-9" />
    </div>
  );
}

function EmptyRow({ icon: Icon, text }: { icon: LucideIcon; text: string }) {
  return (
    <div className="flex items-center gap-3 border-y border-line py-4 text-sm text-fg-faint">
      <Icon className="h-4 w-4 shrink-0" />
      {text}
    </div>
  );
}
