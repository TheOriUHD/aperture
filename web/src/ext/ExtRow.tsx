import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Puzzle,
  ChevronDown,
  Globe,
  Cctv,
  FileCode2,
  Trash2,
  RotateCcw,
  ArrowUpCircle,
  AlertTriangle,
} from "lucide-react";
import clsx from "clsx";
import { api } from "../lib/api";
import { Switch } from "../components/ui/Switch";
import { PermissionList, permissionFlags } from "./PermissionList";
import type { ExtItem, UpdateInfo } from "./types";
import { useExtActions } from "./useExtensions";

type Actions = ReturnType<typeof useExtActions>;

function statePill(ext: ExtItem): { label: string; cls: string } {
  if (!ext.enabled) return { label: "Disabled", cls: "bg-ink-700 text-fg-faint" };
  if (ext.hasBackend) {
    switch (ext.state) {
      case "running":
        return { label: "Running", cls: "bg-online/15 text-online" };
      case "starting":
        return { label: "Starting", cls: "bg-brand/15 text-brand" };
      case "failed":
        return { label: "Crash-looping", cls: "bg-rec/15 text-rec" };
      default:
        return { label: "Stopped", cls: "bg-ink-700 text-fg-muted" };
    }
  }
  return { label: "Active", cls: "bg-online/15 text-online" };
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
  const sp = statePill(ext);
  const flags = permissionFlags(ext.consent);
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
    <div className="overflow-hidden rounded-lg border border-line bg-ink-850">
      <div className="flex items-center gap-3 px-3 py-2.5">
        <div className="grid h-9 w-9 shrink-0 place-items-center overflow-hidden rounded-lg bg-ink-700">
          {ext.icon ? (
            <img src={ext.icon} alt="" className="h-full w-full object-cover" />
          ) : (
            <Puzzle className="h-5 w-5 text-fg-faint" />
          )}
        </div>

        <button onClick={() => setOpen((v) => !v)} className="min-w-0 flex-1 text-left">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="truncate font-medium text-fg">{ext.name}</span>
            <span className="pill bg-ink-700 text-fg-muted">v{ext.version}</span>
            <span className={clsx("pill", sp.cls)}>{sp.label}</span>
            {ext.modified && <span className="pill bg-[#e0a54a]/15 text-[#e6b45a]">Modified</span>}
            {hasUpdate && (
              <span className="pill bg-brand/15 text-brand">
                <ArrowUpCircle className="h-3 w-3" /> Update
              </span>
            )}
            {flags.network && (
              <span className="pill bg-[#e0a54a]/15 text-[#e6b45a]">
                <Globe className="h-3 w-3" /> Network
              </span>
            )}
            {flags.allCameras && (
              <span className="pill bg-[#e0a54a]/15 text-[#e6b45a]">
                <Cctv className="h-3 w-3" /> All cameras
              </span>
            )}
          </div>
          <div className="mt-0.5 truncate text-xs text-fg-muted">
            {ext.author ? `by ${ext.author}` : "Unknown author"} · {ext.source === "store" ? "Store" : "Sideloaded"}
            {ext.pinned ? " · Pinned to sidebar" : ""}
          </div>
        </button>

        <Switch
          checked={ext.enabled}
          disabled={busy}
          onChange={(v) => (v ? actions.enable.mutate(ext.id) : actions.disable.mutate(ext.id))}
        />
        <button
          onClick={() => setOpen((v) => !v)}
          className="btn-icon"
          aria-label={open ? "Collapse" : "Expand"}
        >
          <ChevronDown className={clsx("h-4 w-4 transition-transform", open && "rotate-180")} />
        </button>
      </div>

      {open && (
        <div className="space-y-4 border-t border-line px-3 py-3">
          {ext.state === "failed" && ext.reason && (
            <div className="flex items-start gap-2 rounded-lg border border-rec/30 bg-rec/10 px-3 py-2 text-sm text-fg">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-rec" />
              <span>This extension keeps crashing and has been stopped. {ext.reason}</span>
            </div>
          )}

          {hasUpdate && (
            <div className="flex items-center gap-3 rounded-lg border border-brand/30 bg-brand/10 px-3 py-2.5">
              <ArrowUpCircle className="h-5 w-5 shrink-0 text-brand" />
              <div className="min-w-0 flex-1 text-sm">
                <span className="text-fg">Version {update?.latestVersion} is available</span>
                <span className="text-fg-muted"> · you have {ext.version}</span>
                {update?.permissionsChanged && (
                  <span className="ml-1 text-[#e6b45a]">· needs new permissions</span>
                )}
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

          {ext.canPin && (
            <div className="flex items-center justify-between gap-3 rounded-lg border border-line bg-ink-800 px-3 py-2.5">
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
              <pre className="max-h-40 overflow-auto rounded-lg border border-line bg-ink-900 p-2.5 text-[11px] leading-relaxed text-fg-muted">
                {logs.isLoading
                  ? "Loading…"
                  : logs.data?.lines.length
                    ? logs.data.lines.join("\n")
                    : "No output yet."}
              </pre>
            </div>
          )}

          <div className="flex flex-wrap items-center gap-2 pt-1">
            <button className="btn-ghost h-8" onClick={() => onEdit(ext)}>
              <FileCode2 className="h-4 w-4" /> Edit files
            </button>
            {ext.revertable && (
              <button
                className="btn-ghost h-8"
                disabled={actions.revert.isPending}
                onClick={() => actions.revert.mutate(ext.id)}
              >
                <RotateCcw className="h-4 w-4" /> Revert update
              </button>
            )}
            <button className="btn-ghost !text-rec !border-rec/30 hover:!bg-rec/10 h-8 ml-auto" onClick={() => onUninstall(ext)}>
              <Trash2 className="h-4 w-4" /> Uninstall
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
