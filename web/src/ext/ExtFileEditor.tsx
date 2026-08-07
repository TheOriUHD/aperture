/// <reference types="vite/client" />
//
// ExtFileEditor — a Monaco-backed editor for the files of one installed
// extension. It is intentionally the DEFAULT export and imports Monaco at module
// top level so that a parent which lazy-imports this file (React.lazy / dynamic
// import) pulls Monaco into THIS lazy chunk only, keeping it out of the main
// bundle.
//
// Monaco is fully self-hosted: no CDN fetch happens at runtime. `loader.config`
// hands @monaco-editor/react our locally-bundled monaco instance, and the web
// workers are wired through Vite's `?worker` imports via `self.MonacoEnvironment`
// so every worker is served from our own origin.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import * as monaco from "monaco-editor";
import { Editor, loader, type OnChange, type OnMount } from "@monaco-editor/react";
import editorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import jsonWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";
import cssWorker from "monaco-editor/esm/vs/language/css/css.worker?worker";
import htmlWorker from "monaco-editor/esm/vs/language/html/html.worker?worker";
import tsWorker from "monaco-editor/esm/vs/language/typescript/ts.worker?worker";

import { Modal } from "../components/ui/Modal";
import { Spinner } from "../components/ui/Spinner";
import { api } from "../lib/api";

// Serve Monaco's workers from our own bundle (Vite `?worker` chunks) instead of a
// CDN. `getWorker` picks the language service worker by Monaco's label, falling
// back to the editor worker (which alone still gives syntax highlighting).
self.MonacoEnvironment = {
  getWorker(_workerId: string, label: string): Worker {
    if (label === "json") return new jsonWorker();
    if (label === "css" || label === "scss" || label === "less") return new cssWorker();
    if (label === "html" || label === "handlebars" || label === "razor") return new htmlWorker();
    if (label === "typescript" || label === "javascript") return new tsWorker();
    return new editorWorker();
  },
};

// Use the monaco we just bundled — never let @monaco-editor/react reach for its
// default CDN copy.
loader.config({ monaco });

type ExtFile = { path: string; size: number };
type FileEntry = { value: string; saved: string; binary: boolean };

// Monaco language id from a file extension. Anything unrecognised is plain text.
function langFromPath(path: string): string {
  const dot = path.lastIndexOf(".");
  const ext = dot >= 0 ? path.slice(dot + 1).toLowerCase() : "";
  switch (ext) {
    case "ts":
    case "tsx":
      return "typescript";
    case "js":
    case "jsx":
      return "javascript";
    case "json":
      return "json";
    case "html":
      return "html";
    case "css":
      return "css";
    case "md":
      return "markdown";
    default:
      return "plaintext";
  }
}

// A "source" file is anything with a recognised code language — used only to pick
// a sensible file to open first.
function isSource(path: string): boolean {
  return langFromPath(path) !== "plaintext";
}

// Group the flat, forward-slash file list by its folder, folders and files each
// sorted alphabetically ("" — the repo root — sorts first).
function groupByFolder(files: ExtFile[]): { folder: string; files: { path: string; name: string }[] }[] {
  const map = new Map<string, { path: string; name: string }[]>();
  for (const f of files) {
    const idx = f.path.lastIndexOf("/");
    const folder = idx >= 0 ? f.path.slice(0, idx) : "";
    const name = idx >= 0 ? f.path.slice(idx + 1) : f.path;
    const bucket = map.get(folder);
    if (bucket) bucket.push({ path: f.path, name });
    else map.set(folder, [{ path: f.path, name }]);
  }
  return [...map.keys()]
    .sort((a, b) => a.localeCompare(b))
    .map((folder) => ({
      folder,
      files: map.get(folder)!.sort((a, b) => a.name.localeCompare(b.name)),
    }));
}

export default function ExtFileEditor({
  extId,
  extName,
  onClose,
}: {
  extId: string;
  extName: string;
  onClose: () => void;
}) {
  const [files, setFiles] = useState<ExtFile[]>([]);
  const [contents, setContents] = useState<Record<string, FileEntry>>({});
  const [activePath, setActivePath] = useState<string | null>(null);
  const [pendingPath, setPendingPath] = useState<string | null>(null);

  const [listLoading, setListLoading] = useState(true);
  const [listError, setListError] = useState<string | null>(null);
  const [fileLoading, setFileLoading] = useState(false);
  const [fileError, setFileError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  // Latest active path / loaded files, read from callbacks that must not close
  // over stale state (Monaco's change subscription, the Ctrl/Cmd+S handler).
  const activePathRef = useRef<string | null>(null);
  const contentsRef = useRef<Record<string, FileEntry>>({});
  const saveRef = useRef<() => void>(() => {});
  useEffect(() => {
    activePathRef.current = activePath;
  }, [activePath]);
  useEffect(() => {
    contentsRef.current = contents;
  }, [contents]);

  // Open a file: switch to it if already loaded, else fetch first (keeping the
  // current file — and the mounted Monaco instance — visible until it arrives),
  // then switch. Unsaved edits live in `contents`, so re-opening restores them.
  const openFile = useCallback(
    async (p: string) => {
      if (contentsRef.current[p]) {
        setFileError(null);
        setActivePath(p);
        return;
      }
      setFileLoading(true);
      setFileError(null);
      setPendingPath(p);
      try {
        const res = await api.get<{ path: string; content: string; binary: boolean }>(
          `/api/ext/${extId}/files/${p}`,
        );
        const value = res.content ?? "";
        setContents((prev) => ({ ...prev, [p]: { value, saved: value, binary: !!res.binary } }));
        setActivePath(p);
      } catch (e) {
        setFileError(e instanceof Error ? e.message : "Failed to open file");
      } finally {
        setFileLoading(false);
        setPendingPath(null);
      }
    },
    [extId],
  );

  // Load the flat file list on open and open the first source file by default.
  useEffect(() => {
    let cancelled = false;
    setListLoading(true);
    setListError(null);
    api
      .get<{ files: ExtFile[] }>(`/api/ext/${extId}/files`)
      .then((res) => {
        if (cancelled) return;
        const list = res.files ?? [];
        setFiles(list);
        const def = list.find((f) => isSource(f.path)) ?? list[0];
        if (def) void openFile(def.path);
      })
      .catch((e) => {
        if (!cancelled) setListError(e instanceof Error ? e.message : "Failed to load files");
      })
      .finally(() => {
        if (!cancelled) setListLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [extId, openFile]);

  // Save the active file. Baselines `saved` to the exact bytes we sent, so edits
  // made while the PUT is in flight stay marked dirty.
  const doSave = useCallback(async () => {
    const p = activePathRef.current;
    if (!p) return;
    const entry = contentsRef.current[p];
    if (!entry || entry.binary || entry.value === entry.saved) return;
    const sent = entry.value;
    setSaving(true);
    setSaveError(null);
    try {
      await api.put(`/api/ext/${extId}/files/${p}`, { content: sent });
      setContents((prev) => {
        const cur = prev[p];
        if (!cur) return prev;
        return { ...prev, [p]: { ...cur, saved: sent } };
      });
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : "Failed to save");
    } finally {
      setSaving(false);
    }
  }, [extId]);
  useEffect(() => {
    saveRef.current = () => void doSave();
  }, [doSave]);

  // Cmd/Ctrl+S from anywhere in the modal. Monaco registers the same chord on the
  // editor (below) and marks the event handled when it's focused, so this only
  // fires the fallback when focus is outside the editor — no double save.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === "s" || e.key === "S")) {
        if (e.defaultPrevented) return;
        e.preventDefault();
        saveRef.current();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const onEditorChange = useCallback<OnChange>((val) => {
    const p = activePathRef.current;
    if (!p) return;
    const v = val ?? "";
    setContents((prev) => {
      const cur = prev[p];
      if (!cur || cur.binary || cur.value === v) return prev;
      return { ...prev, [p]: { ...cur, value: v } };
    });
  }, []);

  const onEditorMount: OnMount = (editor) => {
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => saveRef.current());
  };

  const groups = useMemo(() => groupByFolder(files), [files]);
  const activeEntry = activePath ? contents[activePath] : undefined;
  const activeDirty = !!activeEntry && !activeEntry.binary && activeEntry.value !== activeEntry.saved;
  const highlight = pendingPath ?? activePath;

  const footer = (
    <>
      {saveError && (
        <span className="mr-auto self-center truncate text-sm text-rec" title={saveError}>
          {saveError}
        </span>
      )}
      <button className="btn-ghost" onClick={onClose}>
        Close
      </button>
      <button className="btn-primary" onClick={() => void doSave()} disabled={!activeDirty || saving}>
        {saving ? "Saving…" : "Save"}
      </button>
    </>
  );

  return (
    <Modal open onClose={onClose} title={`Edit — ${extName}`} footer={footer} size="xl">
      <div className="flex h-[70vh] min-h-[320px] gap-3">
        {/* File tree — grouped by folder, monospace, click to open. */}
        <div className="w-40 shrink-0 overflow-y-auto rounded-md border border-line bg-ink-850 p-1 font-mono text-xs">
          {listLoading ? (
            <div className="grid h-full place-items-center">
              <Spinner />
            </div>
          ) : listError ? (
            <div className="p-2 text-sm text-rec">{listError}</div>
          ) : files.length === 0 ? (
            <div className="p-2 text-fg-faint">No files.</div>
          ) : (
            groups.map((g) => (
              <div key={g.folder || "/"} className="mb-1">
                {g.folder && (
                  <div
                    className="truncate px-2 py-1 text-[10px] uppercase tracking-wide text-fg-faint"
                    title={g.folder}
                  >
                    {g.folder}
                  </div>
                )}
                {g.files.map((f) => {
                  const entry = contents[f.path];
                  const dirty = !!entry && !entry.binary && entry.value !== entry.saved;
                  const active = highlight === f.path;
                  return (
                    <button
                      key={f.path}
                      onClick={() => void openFile(f.path)}
                      title={f.path}
                      className={clsx(
                        "flex w-full items-center gap-1.5 rounded px-2 py-1 text-left",
                        g.folder && "pl-3",
                        active ? "bg-brand/15 text-fg" : "text-fg-muted hover:bg-ink-700 hover:text-fg",
                      )}
                    >
                      <span className="truncate">{f.name}</span>
                      {dirty && (
                        <span className="ml-auto h-1.5 w-1.5 shrink-0 rounded-full bg-brand" title="Unsaved changes" />
                      )}
                    </button>
                  );
                })}
              </div>
            ))
          )}
        </div>

        {/* Editor pane. */}
        <div className="relative min-w-0 flex-1 overflow-hidden rounded-md border border-line bg-ink-850">
          {(() => {
            if (!activePath && listLoading) {
              return (
                <div className="grid h-full place-items-center">
                  <Spinner />
                </div>
              );
            }
            if (fileError && !activeEntry) {
              return <div className="grid h-full place-items-center p-4 text-sm text-rec">{fileError}</div>;
            }
            if (activeEntry?.binary) {
              return (
                <div className="grid h-full place-items-center p-4 text-sm text-fg-faint">
                  binary file — not editable
                </div>
              );
            }
            if (activeEntry) {
              return (
                <Editor
                  path={activePath ?? undefined}
                  language={langFromPath(activePath ?? "")}
                  value={activeEntry.value}
                  theme="vs-dark"
                  loading={<Spinner />}
                  height="100%"
                  options={{
                    minimap: { enabled: false },
                    fontSize: 13,
                    // Spec said `scrollBeyondLastFile`; the real Monaco option is
                    // `scrollBeyondLastLine` (the former isn't a valid key and
                    // would fail the typed-props check).
                    scrollBeyondLastLine: false,
                    automaticLayout: true,
                  }}
                  onChange={onEditorChange}
                  onMount={onEditorMount}
                />
              );
            }
            return <div className="grid h-full place-items-center p-4 text-sm text-fg-faint">No file open.</div>;
          })()}
          {fileLoading && (
            <div className="absolute right-2 top-2 rounded bg-ink-900/80 px-2 py-1">
              <Spinner />
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
}
