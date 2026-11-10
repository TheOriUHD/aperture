import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, FileCode2, Trash2, RotateCcw, ArrowUpCircle, AlertTriangle } from "lucide-react";
import clsx from "clsx";
import { api } from "../lib/api";
import { Switch } from "../components/ui/Switch";
import { ExtIcon } from "./ExtIcon";
import { PermissionList } from "./PermissionList";
import { CameraGrantControl, initialGrantValue } from "./CameraGrants";
import { ExtSettingsForm } from "./ExtSettingsForm";
import type { ExtItem, UpdateInfo } from "./types";
import { useExtActions } from "./useExtensions";

type Actions = ReturnType<typeof useExtActions>;

// StatusDot keeps colour rare: normal states are a small muted dot + label; only a
// crash-loop escalates to a red pill (the one attention colour a row can carry).
// "Update available" is shown separately as a single blue pill.
function StatusDot({ ext }: { ext: ExtItem }) {
  if (!ext.enabled) return <span className="text-sm text-fg-faint">Disabled</span>;
  if (ext.hasBackend && ext.state === "failed") {
    return (
      <span className="pill-red">
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
      color = "bg-warn";
      label = "Starting";
    } else {
      color = "bg-fg-faint";
      label = "Stopped";
    }
  }
  return (
    <span className="inline-flex items-center gap-2 text-sm text-fg/80">
      <span className={clsx("h-2 w-2 shrink-0 rounded-full", color)} />
      {label}
    </span>
  );
}

// The manager's column layout, shared by its header row and every ExtRow so the
// rows line up like a table (a <table> can't host the per-row error boundary).
export const EXT_COLS = {
  version: "hidden w-24 shrink-0 md:block",
  status: "w-[92px] shrink-0 sm:w-36",
  enabled: "w-10 shrink-0 sm:w-14",
  expand: "w-8 shrink-0",
} as const;

export function ExtRow({
  ext,
  actions,
  update,
  onUpdate,
  updating,
  onEdit,
  onUninstall,
}: {
  ext: ExtItem;
  actions: Actions;
  update?: UpdateInfo;
  onUpdate: (ext: ExtItem) => void;
  onEdit: (ext: ExtItem) => void;
  onUninstall: (ext: ExtItem) => void;
  /** True while the manager is staging this update, before applyUpdate starts. */
  updating?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const busy =
    actions.enable.isPending ||
    actions.disable.isPending ||
    actions.pin.isPending ||
    actions.unpin.isPending ||
    // An update stops the backend, swaps its files and respawns it. Firing a second
    // one while the first is mid-swap makes the two race over the same directories
    // and can leave the extension stopped — which is why this button, alone among
    // the row actions, used to need a disable/enable afterwards to recover.
    actions.applyUpdate.isPending ||
    updating;
  const hasUpdate = !!update?.available;

  const logs = useQuery({
    queryKey: ["ext-logs", ext.id],
    queryFn: () => api.get<{ lines: string[] }>(`/api/ext/${ext.id}/logs`),
    enabled: open && ext.hasBackend,
    refetchInterval: open && ext.state === "running" ? 3000 : false,
  });

  const toggleOpen = () => setOpen((v) => !v);

  return (
    <div className={clsx("border-b border-line", open && "pb-4")}>
      <div
        onClick={toggleOpen}
        className={clsx(
          "-mx-2 flex min-h-14 cursor-pointer items-center gap-3 rounded-md px-2 py-2 transition-colors hover:bg-white/[0.025]",
          !ext.enabled && "[&>*:not(.ext-switch)]:opacity-60",
        )}
      >
        <ExtIcon icon={ext.icon} name={ext.name} size={32} />

        <button type="button" aria-expanded={open} className="min-w-0 flex-1 text-left">
          <div className="flex items-center gap-2">
            <span className="truncate text-sm font-medium text-fg">{ext.name}</span>
            {hasUpdate && (
              <span className="pill-blue shrink-0">
                <ArrowUpCircle className="h-3 w-3" /> Update
              </span>
            )}
          </div>
          <div className="mt-0.5 truncate text-xs text-fg-muted">
            <span className="tabular-nums md:hidden">v{ext.version} · </span>
            {ext.author ? `by ${ext.author}` : "Unknown author"} · {ext.source === "store" ? "Store" : "Sideloaded"}
            {ext.modified && " · Edited"}
            {ext.pinned && " · Pinned"}
          </div>
        </button>

        <span className={clsx(EXT_COLS.version, "text-sm tabular-nums text-fg/80")}>v{ext.version}</span>
        <div className={EXT_COLS.status}>
          <StatusDot ext={ext} />
        </div>
        <div className={clsx(EXT_COLS.enabled, "ext-switch")} onClick={(e) => e.stopPropagation()}>
          <Switch
            checked={ext.enabled}
            disabled={busy}
            onChange={(v) => (v ? actions.enable.mutate(ext.id) : actions.disable.mutate(ext.id))}
          />
        </div>
        <button type="button" className={clsx(EXT_COLS.expand, "btn-icon")} aria-label={open ? "Collapse" : "Expand"}>
          <ChevronDown className={clsx("h-4 w-4 transition-transform", open && "rotate-180")} />
        </button>
      </div>

      {open && (
        <div className="mt-2 divide-y divide-line rounded-lg bg-ink-800 px-4 [&>*]:py-4">
          {ext.state === "failed" && ext.reason && (
            <div>
              <div className="flex items-start gap-2.5 rounded-md border border-danger/30 bg-danger/[0.08] px-3 py-2.5 text-sm text-fg">
                <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-danger" />
                <span>This extension keeps crashing and has been stopped. {ext.reason}</span>
              </div>
            </div>
          )}

          {hasUpdate && (
            <div className="flex items-center gap-3">
              <ArrowUpCircle className="h-5 w-5 shrink-0 text-brand" />
              <div className="min-w-0 flex-1 text-sm">
                <span className="text-fg">Version {update?.latestVersion} available</span>
                <span className="text-fg-muted"> · you have {ext.version}</span>
                {update?.permissionsChanged && <span className="ml-1 text-warn">· needs new permissions</span>}
              </div>
              <button className="btn-primary" disabled={busy} onClick={() => onUpdate(ext)}>
                {busy ? "Updating…" : "Update"}
              </button>
            </div>
          )}

          <div>
            <p className="mb-2.5 text-[13px] font-semibold text-fg">Permissions</p>
            <PermissionList lines={ext.consent} />
          </div>

          {ext.hasSettings && <ExtSettingsForm ext={ext} />}

          {ext.requestsCameras && <CameraEdit ext={ext} actions={actions} />}

          {ext.canPin && (
            <div className="flex items-center justify-between gap-4">
              <div className="min-w-0">
                <p className="text-[13px] font-semibold text-fg">Pin to sidebar</p>
                <p className="mt-0.5 text-xs text-fg-faint">Give this extension its own icon and full-page view in the nav rail.</p>
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
              <p className="mb-2.5 text-[13px] font-semibold text-fg">Logs</p>
              <pre className="max-h-48 overflow-auto rounded-md border border-line bg-ink-900 p-3 font-mono text-[11px] leading-relaxed text-fg-muted">
                {logs.isLoading ? "Loading…" : logs.data?.lines.length ? logs.data.lines.join("\n") : "No output yet."}
              </pre>
            </div>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <button className="btn-ghost" onClick={() => onEdit(ext)}>
              <FileCode2 className="h-4 w-4" /> Edit files
            </button>
            {ext.revertable && (
              <button className="btn-ghost" disabled={actions.revert.isPending} onClick={() => actions.revert.mutate(ext.id)}>
                <RotateCcw className="h-4 w-4" /> Revert update
              </button>
            )}
            <button
              className="btn-ghost ml-auto border-danger/40 text-danger hover:border-danger/70 hover:bg-danger/10"
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
    <div>
      <div className="mb-3 flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-[13px] font-semibold text-fg">Cameras</p>
          <p className="mt-0.5 text-xs text-fg-faint">Which cameras this extension can see. Changing it re-scopes the extension — no re-consent.</p>
        </div>
        <button
          className="btn-subtle shrink-0"
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
