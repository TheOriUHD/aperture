import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, FileCode2, Trash2, RotateCcw, ArrowUpCircle, AlertTriangle } from "lucide-react";
import clsx from "clsx";
import { api } from "../lib/api";
import { Switch } from "../components/ui/Switch";
import { ExtIcon } from "./ExtIcon";
import { PermissionList } from "./PermissionList";
import { CameraGrantControl, initialGrantValue } from "./CameraGrants";
import type { ExtItem, UpdateInfo } from "./types";
import { useExtActions } from "./useExtensions";

type Actions = ReturnType<typeof useExtActions>;

// StatusDot keeps colour rare: normal states are a small muted dot + label; only a
// crash-loop escalates to a red pill (the one attention colour a row can carry).
// "Update available" is shown separately as a single blue pill.
function StatusDot({ ext }: { ext: ExtItem }) {
  if (!ext.enabled) return <span className="text-xs text-fg-faint">Disabled</span>;
  if (ext.hasBackend && ext.state === "failed") {
    return (
      <span className="pill bg-rec/15 text-rec">
        <AlertTriangle className="h-3 w-3" /> Needs attention
      </span>
    );
  }
  let color = "bg-online";
  let label = "Active";
  if (ext.hasBackend) {
    if (ext.state === "running") {
      label = "Running";
    } else if (ext.state === "starting") {
      color = "bg-[#e6b45a]";
      label = "Starting";
    } else {
      color = "bg-fg-faint";
      label = "Stopped";
    }
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-fg-muted">
      <span className={clsx("h-1.5 w-1.5 rounded-full", color)} />
      {label}
    </span>
  );
}

export function ExtRow({
  ext,
  actions,
  update,
  onUpdate,
  onEdit,
  onUninstall,
}: {
  ext: ExtItem;
  actions: Actions;
  update?: UpdateInfo;
  onUpdate: (ext: ExtItem) => void;
  onEdit: (ext: ExtItem) => void;
  onUninstall: (ext: ExtItem) => void;
}) {
  const [open, setOpen] = useState(false);
  const busy =
    actions.enable.isPending || actions.disable.isPending || actions.pin.isPending || actions.unpin.isPending;
  const hasUpdate = !!update?.available;

  const logs = useQuery({
    queryKey: ["ext-logs", ext.id],
    queryFn: () => api.get<{ lines: string[] }>(`/api/ext/${ext.id}/logs`),
    enabled: open && ext.hasBackend,
    refetchInterval: open && ext.state === "running" ? 3000 : false,
  });

  return (
    <div className={clsx("rounded-xl border border-line bg-ink-850 transition-colors", !ext.enabled && "opacity-60")}>
      <div className="flex items-center gap-3 p-3.5">
        <ExtIcon icon={ext.icon} name={ext.name} size={40} />

        <button onClick={() => setOpen((v) => !v)} className="min-w-0 flex-1 text-left">
          <div className="flex items-center gap-2">
            <span className="truncate text-sm font-medium text-fg">{ext.name}</span>
            <span className="shrink-0 text-xs tabular-nums text-fg-faint">v{ext.version}</span>
          </div>
          <div className="mt-0.5 truncate text-xs text-fg-muted">
            {ext.author ? `by ${ext.author}` : "Unknown author"} · {ext.source === "store" ? "Store" : "Sideloaded"}
            {ext.modified && " · Edited"}
            {ext.pinned && " · Pinned"}
          </div>
        </button>

        <div className="flex shrink-0 items-center gap-3">
          {hasUpdate && (
            <span className="pill bg-brand/15 text-brand">
              <ArrowUpCircle className="h-3 w-3" /> Update
            </span>
          )}
          <StatusDot ext={ext} />
          <Switch
            checked={ext.enabled}
            disabled={busy}
            onChange={(v) => (v ? actions.enable.mutate(ext.id) : actions.disable.mutate(ext.id))}
          />
          <button onClick={() => setOpen((v) => !v)} className="btn-icon" aria-label={open ? "Collapse" : "Expand"}>
            <ChevronDown className={clsx("h-4 w-4 transition-transform", open && "rotate-180")} />
          </button>
        </div>
      </div>

      {open && (
        <div className="space-y-4 border-t border-line px-3.5 pb-3.5 pt-3.5">
          {ext.state === "failed" && ext.reason && (
            <div className="flex items-start gap-2 rounded-lg border border-rec/30 bg-rec/10 px-3 py-2 text-sm text-fg">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-rec" />
              <span>This extension keeps crashing and has been stopped. {ext.reason}</span>
            </div>
          )}

          {hasUpdate && (
            <div className="flex items-center gap-3 rounded-lg border border-brand/25 bg-brand/[0.07] px-3 py-2.5">
              <ArrowUpCircle className="h-5 w-5 shrink-0 text-brand" />
              <div className="min-w-0 flex-1 text-sm">
                <span className="text-fg">Version {update?.latestVersion} available</span>
                <span className="text-fg-muted"> · you have {ext.version}</span>
                {update?.permissionsChanged && <span className="ml-1 text-[#e6b45a]">· needs new permissions</span>}
              </div>
              <button className="btn-subtle h-8" onClick={() => onUpdate(ext)}>
                Update
              </button>
            </div>
          )}

          <div>
            <p className="label">Permissions</p>
            <PermissionList lines={ext.consent} />
          </div>

          {ext.requestsCameras && <CameraEdit ext={ext} actions={actions} />}

          {ext.canPin && (
            <div className="flex items-center justify-between gap-3 rounded-lg border border-line bg-ink-900/40 px-3 py-2.5">
              <div className="min-w-0">
                <p className="text-sm text-fg">Pin to sidebar</p>
                <p className="text-xs text-fg-muted">Give this extension its own icon and full-page view in the nav rail.</p>
              </div>
              <Switch
                checked={ext.pinned}
                disabled={busy || !ext.enabled}
                onChange={(v) => (v ? actions.pin.mutate(ext.id) : actions.unpin.mutate(ext.id))}
              />
            </div>
          )}

          {ext.hasBackend && (
            <div>
              <p className="label">Logs</p>
              <pre className="max-h-40 overflow-auto rounded-lg border border-line bg-ink-900 p-2.5 font-mono text-[11px] leading-relaxed text-fg-muted">
                {logs.isLoading ? "Loading…" : logs.data?.lines.length ? logs.data.lines.join("\n") : "No output yet."}
              </pre>
            </div>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <button className="btn-ghost h-8" onClick={() => onEdit(ext)}>
              <FileCode2 className="h-4 w-4" /> Edit files
            </button>
            {ext.revertable && (
              <button className="btn-ghost h-8" disabled={actions.revert.isPending} onClick={() => actions.revert.mutate(ext.id)}>
                <RotateCcw className="h-4 w-4" /> Revert update
              </button>
            )}
            <button
              className="btn-ghost h-8 ml-auto !border-rec/30 !text-rec hover:!bg-rec/10"
              onClick={() => onUninstall(ext)}
            >
              <Trash2 className="h-4 w-4" /> Uninstall
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

// CameraEdit re-scopes an installed extension's cameras from its row. The manifest
// request is the ceiling; the user narrows within it. Saving re-mints the token (no
// re-consent — camera scope is the user's decision, not a new permission).
function CameraEdit({ ext, actions }: { ext: ExtItem; actions: Actions }) {
  // The API returns [] for an all-cameras request/grant; be defensive about null too.
  const grants = ext.cameraGrants ?? [];
  const requested = ext.requestedCameras ?? [];
  const requestAll = ext.requestsCameras && requested.length === 0;
  const grantsKey = grants.join(",");
  const [grant, setGrant] = useState<string[] | null>(() => initialGrantValue(requestAll, requested, grants));
  useEffect(() => {
    setGrant(initialGrantValue(requestAll, requested, grants));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ext.id, grantsKey, requestAll]);

  const currentIsAll = grants.length === 0;
  const dirty =
    grant === null
      ? !currentIsAll
      : currentIsAll || [...grant].sort().join(",") !== [...grants].sort().join(",");
  const canSave = (grant === null || grant.length > 0) && dirty;

  return (
    <div className="rounded-lg border border-line bg-ink-900/40 p-3">
      <div className="mb-2 flex items-center justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm text-fg">Cameras</p>
          <p className="text-[11px] text-fg-faint">Which cameras this extension can see. Changing it re-scopes the extension — no re-consent.</p>
        </div>
        <button
          className="btn-subtle h-8 shrink-0"
          disabled={!canSave || actions.setCameras.isPending}
          onClick={() => actions.setCameras.mutate({ id: ext.id, cameras: grant })}
        >
          {actions.setCameras.isPending ? "Saving…" : "Save"}
        </button>
      </div>
      <CameraGrantControl requestsCameras={ext.requestsCameras} requestedCameras={requested} value={grant} onChange={setGrant} />
    </div>
  );
}
