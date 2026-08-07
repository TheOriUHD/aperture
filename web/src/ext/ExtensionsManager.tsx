import { Suspense, lazy, useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Puzzle, Plus, AlertTriangle } from "lucide-react";
import { ApiError } from "../lib/api";
import { EmptyState } from "../components/ui/EmptyState";
import { ConfirmDialog } from "../components/ui/ConfirmDialog";
import { ErrorBoundary } from "../components/ui/ErrorBoundary";
import { Skeleton } from "../components/ui/Skeleton";
import { ExtRow } from "./ExtRow";
import { Marketplace } from "./Marketplace";
import { ConsentModal } from "./ConsentModal";
import { useExtensions, useExtActions, stageUpload, stageUrl, stageStore, confirmInstall, checkUpdate } from "./useExtensions";
import type { ExtItem, StageView, UpdateInfo } from "./types";

// Monaco is heavy; load it only when the editor is actually opened.
const ExtFileEditor = lazy(() => import("./ExtFileEditor"));

function errMsg(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  return e instanceof Error ? e.message : "Something went wrong";
}

// Background update checks for every extension with an update source — non-blocking
// and silent on failure, so an update surfaces only as a quiet pill.
function useUpdateChecks(exts: ExtItem[] | undefined): Record<string, UpdateInfo> {
  const [map, setMap] = useState<Record<string, UpdateInfo>>({});
  const ids = (exts ?? []).filter((e) => e.updatable).map((e) => e.id).join(",");
  useEffect(() => {
    let cancelled = false;
    for (const e of (exts ?? []).filter((x) => x.updatable)) {
      checkUpdate(e.id)
        .then((info) => !cancelled && setMap((m) => ({ ...m, [e.id]: info })))
        .catch(() => {});
    }
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ids]);
  return map;
}

interface Staged {
  view: StageView;
  mode: "install" | "update";
  targetId?: string;
}

export function ExtensionsManager() {
  const qc = useQueryClient();
  const { data: exts, isLoading, isError, error } = useExtensions();
  const actions = useExtActions();
  const updates = useUpdateChecks(exts);

  const [market, setMarket] = useState(false);
  const [staged, setStaged] = useState<Staged | null>(null);
  const [staging, setStaging] = useState(false);
  const [confirmBusy, setConfirmBusy] = useState(false);
  const [stageErr, setStageErr] = useState<string>();
  const [uninstall, setUninstall] = useState<ExtItem | null>(null);
  const [editing, setEditing] = useState<ExtItem | null>(null);

  const invalidate = () => {
    for (const k of ["ext", "ext-panels", "ext-nav", "ext-store"]) qc.invalidateQueries({ queryKey: [k] });
  };

  async function runStage(fn: () => Promise<StageView>) {
    setStaging(true);
    setStageErr(undefined);
    try {
      setStaged({ view: await fn(), mode: "install" });
    } catch (e) {
      setStageErr(errMsg(e));
    } finally {
      setStaging(false);
    }
  }

  async function onUpdate(ext: ExtItem) {
    const info = updates[ext.id];
    if (!info?.bundleUrl) return;
    setStaging(true);
    setStageErr(undefined);
    try {
      const view = await stageUrl(info.bundleUrl);
      if (info.permissionsChanged) {
        setStaged({ view, mode: "update", targetId: ext.id }); // force fresh consent
      } else {
        setConfirmBusy(true);
        await actions.applyUpdate.mutateAsync({ id: ext.id, stagingId: view.stagingId, consented: true });
      }
    } catch (e) {
      setStageErr(errMsg(e));
    } finally {
      setStaging(false);
      setConfirmBusy(false);
    }
  }

  async function onConfirm(cameras: string[] | null | undefined) {
    if (!staged) return;
    setConfirmBusy(true);
    setStageErr(undefined);
    try {
      if (staged.mode === "install") {
        await confirmInstall(staged.view.stagingId, cameras);
        invalidate();
      } else {
        await actions.applyUpdate.mutateAsync({ id: staged.targetId!, stagingId: staged.view.stagingId, consented: true });
      }
      setStaged(null); // leave the marketplace open so more can be added
    } catch (e) {
      setStageErr(errMsg(e));
    } finally {
      setConfirmBusy(false);
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-medium text-fg">Extensions</h2>
          <p className="mt-0.5 text-xs text-fg-muted">
            Panels and background automations, each sandboxed to the permissions you approve.
          </p>
        </div>
        <button className="btn-primary h-8 shrink-0" onClick={() => setMarket(true)}>
          <Plus className="h-4 w-4" /> Add
        </button>
      </div>

      {isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <RowSkeleton key={i} />
          ))}
        </div>
      ) : isError ? (
        <p className="rounded-lg border border-rec/30 bg-rec/10 px-3 py-2.5 text-sm text-fg">
          Couldn't load extensions: {errMsg(error)}
        </p>
      ) : !exts?.length ? (
        <div className="rounded-xl border border-line bg-ink-850/50 py-4">
          <EmptyState
            icon={Puzzle}
            title="No extensions yet"
            hint="Add panels and background automations to Aperture. Browse the store or install your own."
            action={
              <button className="btn-primary h-9" onClick={() => setMarket(true)}>
                <Plus className="h-4 w-4" /> Add your first extension
              </button>
            }
          />
        </div>
      ) : (
        <div className="space-y-2">
          {exts.map((ext) => (
            <ErrorBoundary key={ext.id} fallback={() => <ExtRowError ext={ext} />}>
              <ExtRow
                ext={ext}
                actions={actions}
                update={updates[ext.id]}
                onUpdate={onUpdate}
                onEdit={setEditing}
                onUninstall={setUninstall}
              />
            </ErrorBoundary>
          ))}
        </div>
      )}

      <Marketplace
        open={market}
        onClose={() => setMarket(false)}
        onStageUpload={(f) => runStage(() => stageUpload(f))}
        onStageUrl={(u) => runStage(() => stageUrl(u))}
        onStageStore={(id) => runStage(() => stageStore(id))}
        staging={staging}
        error={staged ? undefined : stageErr}
      />

      {/* Consent renders after the marketplace, so it stacks above it. */}
      <ConsentModal
        stage={staged?.view ?? null}
        mode={staged?.mode}
        busy={confirmBusy}
        error={staged ? stageErr : undefined}
        onConfirm={onConfirm}
        onCancel={() => {
          setStaged(null);
          setStageErr(undefined);
        }}
      />

      <ConfirmDialog
        open={!!uninstall}
        danger
        busy={actions.uninstall.isPending}
        title="Uninstall extension?"
        message={`Uninstall "${uninstall?.name}"? Its files and stored data are deleted. This cannot be undone.`}
        confirmLabel="Uninstall"
        onConfirm={async () => {
          if (uninstall) await actions.uninstall.mutateAsync(uninstall.id);
          setUninstall(null);
        }}
        onCancel={() => setUninstall(null)}
      />

      {editing && (
        <Suspense
          fallback={
            <div className="fixed inset-0 z-50 grid place-items-center bg-black/60">
              <div className="text-sm text-fg-muted">Loading editor…</div>
            </div>
          }
        >
          <ExtFileEditor extId={editing.id} extName={editing.name} onClose={() => setEditing(null)} />
        </Suspense>
      )}
    </div>
  );
}

// ExtRowError is the per-row error-boundary fallback: one malformed extension shows
// an inline card instead of taking down the whole manager.
function ExtRowError({ ext }: { ext: ExtItem }) {
  return (
    <div className="flex items-start gap-2.5 rounded-xl border border-rec/30 bg-rec/10 px-3.5 py-3 text-sm">
      <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-rec" />
      <div className="min-w-0">
        <p className="text-fg">“{ext.name || ext.id}” couldn't be displayed.</p>
        <p className="mt-0.5 text-xs text-fg-muted">Its data looks malformed. Other extensions are unaffected.</p>
      </div>
    </div>
  );
}

function RowSkeleton() {
  return (
    <div className="flex items-center gap-3 rounded-xl border border-line bg-ink-850 p-3.5">
      <Skeleton className="h-10 w-10 rounded-lg" />
      <div className="flex-1 space-y-2">
        <Skeleton className="h-3.5 w-40" />
        <Skeleton className="h-3 w-28" />
      </div>
      <Skeleton className="h-6 w-11 rounded-full" />
    </div>
  );
}
