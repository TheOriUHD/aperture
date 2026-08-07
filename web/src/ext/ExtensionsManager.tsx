import { Suspense, lazy, useEffect, useRef, useState, type ChangeEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Puzzle, Upload, Link2 } from "lucide-react";
import { ApiError } from "../lib/api";
import { Spinner } from "../components/ui/Spinner";
import { EmptyState } from "../components/ui/EmptyState";
import { ConfirmDialog } from "../components/ui/ConfirmDialog";
import { ExtRow } from "./ExtRow";
import { StoreSection } from "./StoreSection";
import { ConsentModal } from "./ConsentModal";
import {
  useExtensions,
  useExtActions,
  stageUpload,
  stageUrl,
  stageStore,
  confirmInstall,
  checkUpdate,
} from "./useExtensions";
import type { ExtItem, StageView, UpdateInfo } from "./types";

// Monaco is heavy; keep it out of the main bundle and load it only when the editor
// is actually opened.
const ExtFileEditor = lazy(() => import("./ExtFileEditor"));

function errMsg(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  return e instanceof Error ? e.message : "Something went wrong";
}

// useUpdateChecks fires an update check for every extension with an update source,
// in the background, as soon as the list loads. Non-blocking and silent on failure
// — an update surfaces only as a quiet pill, never a nag or a spinner.
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

  const [staged, setStaged] = useState<Staged | null>(null);
  const [staging, setStaging] = useState(false); // fetching/reading a bundle
  const [confirmBusy, setConfirmBusy] = useState(false); // committing install/update
  const [stageErr, setStageErr] = useState<string>();
  const [uninstall, setUninstall] = useState<ExtItem | null>(null);
  const [editing, setEditing] = useState<ExtItem | null>(null);
  const [urlOpen, setUrlOpen] = useState(false);
  const [url, setUrl] = useState("");
  const fileRef = useRef<HTMLInputElement>(null);

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["ext"] });
    qc.invalidateQueries({ queryKey: ["ext-panels"] });
    qc.invalidateQueries({ queryKey: ["ext-nav"] });
    qc.invalidateQueries({ queryKey: ["ext-store"] });
  };

  async function runStage(fn: () => Promise<StageView>) {
    setStaging(true);
    setStageErr(undefined);
    try {
      const view = await fn();
      setStaged({ view, mode: "install" });
    } catch (e) {
      setStageErr(errMsg(e));
    } finally {
      setStaging(false);
    }
  }

  function onPickFile(e: ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0];
    if (f) runStage(() => stageUpload(f));
    e.target.value = "";
  }
  function onSubmitUrl() {
    const u = url.trim();
    if (!u) return;
    setUrl("");
    setUrlOpen(false);
    runStage(() => stageUrl(u));
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

  async function onConfirm() {
    if (!staged) return;
    setConfirmBusy(true);
    setStageErr(undefined);
    try {
      if (staged.mode === "install") {
        await confirmInstall(staged.view.stagingId);
        invalidate();
      } else {
        await actions.applyUpdate.mutateAsync({
          id: staged.targetId!,
          stagingId: staged.view.stagingId,
          consented: true,
        });
      }
      setStaged(null);
    } catch (e) {
      setStageErr(errMsg(e));
    } finally {
      setConfirmBusy(false);
    }
  }

  return (
    <div className="space-y-5">
      {/* Install controls */}
      <div className="panel">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="text-sm font-medium text-fg">Installed extensions</h2>
            <p className="mt-0.5 max-w-md text-xs text-fg-muted">
              Panels and background automations. Each runs sandboxed with only the permissions you approve.
            </p>
          </div>
          <div className="flex items-center gap-2">
            <button className="btn-ghost h-8" onClick={() => setUrlOpen((v) => !v)} disabled={staging}>
              <Link2 className="h-4 w-4" /> From URL
            </button>
            <button className="btn-primary h-8" onClick={() => fileRef.current?.click()} disabled={staging}>
              <Upload className="h-4 w-4" /> {staging ? "Reading…" : "Install file"}
            </button>
            <input ref={fileRef} type="file" accept=".zip" hidden onChange={onPickFile} />
          </div>
        </div>
        {urlOpen && (
          <div className="mt-3 flex gap-2">
            <input
              className="input"
              placeholder="https://…/bundle.zip"
              value={url}
              autoFocus
              onChange={(e) => setUrl(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && onSubmitUrl()}
            />
            <button className="btn-subtle h-9 shrink-0" onClick={onSubmitUrl} disabled={staging}>
              Fetch
            </button>
          </div>
        )}
        {stageErr && !staged && <p className="mt-2 text-sm text-rec">{stageErr}</p>}
      </div>

      {/* Installed list */}
      {isLoading ? (
        <div className="grid place-items-center py-12">
          <Spinner label="Loading extensions" />
        </div>
      ) : isError ? (
        <p className="rounded-lg border border-rec/30 bg-rec/10 px-3 py-2.5 text-sm text-fg">
          Couldn't load extensions: {errMsg(error)}
        </p>
      ) : !exts?.length ? (
        <EmptyState
          icon={Puzzle}
          title="No extensions installed"
          hint="Install one from a file or the store below to add panels and automations."
        />
      ) : (
        <div className="space-y-2">
          {exts.map((ext) => (
            <ExtRow
              key={ext.id}
              ext={ext}
              actions={actions}
              update={updates[ext.id]}
              onUpdate={onUpdate}
              onEdit={setEditing}
              onUninstall={setUninstall}
            />
          ))}
        </div>
      )}

      {/* Store */}
      <StoreSection onInstall={(id) => runStage(() => stageStore(id))} busy={staging} />

      {/* Consent (install + update re-consent) */}
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

      {/* Uninstall */}
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

      {/* File editor (lazy Monaco) */}
      {editing && (
        <Suspense
          fallback={
            <div className="fixed inset-0 z-50 grid place-items-center bg-black/60">
              <Spinner label="Loading editor" />
            </div>
          }
        >
          <ExtFileEditor extId={editing.id} extName={editing.name} onClose={() => setEditing(null)} />
        </Suspense>
      )}
    </div>
  );
}
