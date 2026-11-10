import { useMemo, useRef, useState, type ChangeEvent } from "react";
import { Upload, Link2, Search, Store, ShieldCheck, Check } from "lucide-react";
import clsx from "clsx";
import { Modal } from "../components/ui/Modal";
import { Spinner } from "../components/ui/Spinner";
import { Skeleton } from "../components/ui/Skeleton";
import { ExtIcon } from "./ExtIcon";
import { useExtStore } from "./useExtensions";
import type { StoreItem } from "./types";

type Filter = "all" | "available" | "installed";
type Sort = "name" | "author";

// Marketplace — the "+" surface. A wide overlay (the shared Modal) with "install
// your own" up top and an instant-search store list below. It only
// stages; the parent's consent modal renders over it and does the committing.
export function Marketplace({
  open,
  onClose,
  onStageUpload,
  onStageUrl,
  onStageStore,
  staging,
  error,
}: {
  open: boolean;
  onClose: () => void;
  onStageUpload: (file: File) => void;
  onStageUrl: (url: string) => void;
  onStageStore: (id: string) => void;
  staging: boolean;
  error?: string;
}) {
  const store = useExtStore(open);
  const fileRef = useRef<HTMLInputElement>(null);
  const [url, setUrl] = useState("");
  const [q, setQ] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [sort, setSort] = useState<Sort>("name");

  const items = store.data?.extensions ?? [];
  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const out = items.filter((it) => {
      if (filter === "installed" && !it.installed) return false;
      if (filter === "available" && it.installed) return false;
      if (!needle) return true;
      return (
        it.name.toLowerCase().includes(needle) ||
        (it.author ?? "").toLowerCase().includes(needle) ||
        (it.description ?? "").toLowerCase().includes(needle)
      );
    });
    out.sort((a, b) =>
      sort === "author"
        ? (a.author ?? "").localeCompare(b.author ?? "") || a.name.localeCompare(b.name)
        : a.name.localeCompare(b.name),
    );
    return out;
  }, [items, q, filter, sort]);

  function onFile(e: ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0];
    if (f) onStageUpload(f);
    e.target.value = "";
  }
  function submitUrl() {
    const u = url.trim();
    if (!u) return;
    setUrl("");
    onStageUrl(u);
  }

  const configured = store.data?.configured;
  const showControls = configured && items.length > 0;

  return (
    <Modal open={open} onClose={onClose} title="Add extensions" size="xl">
      <div className="space-y-6">
        {/* Install your own — real presence, but compact and above the store. */}
        <section className="rounded-lg border border-line p-4">
          <div className="flex items-start gap-2.5">
            <Upload className="mt-0.5 h-4 w-4 shrink-0 text-fg-muted" />
            <div className="min-w-0">
              <h3 className="text-[13px] font-semibold text-fg">Install your own</h3>
              <p className="mt-0.5 text-xs text-fg-faint">From a bundle file, or a URL you trust. Sideloaded extensions carry a warning.</p>
            </div>
          </div>
          <div className="mt-3 flex flex-col gap-2 sm:flex-row">
            <button className="btn-ghost sm:w-36" onClick={() => fileRef.current?.click()} disabled={staging}>
              <Upload className="h-4 w-4" /> {staging ? "Reading…" : "Upload file"}
            </button>
            <input ref={fileRef} type="file" accept=".zip" hidden onChange={onFile} />
            <div className="flex min-w-0 flex-1 gap-2">
              <div className="relative min-w-0 flex-1">
                <Link2 className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-faint" />
                <input
                  className="input pl-9 font-mono text-xs"
                  placeholder="https://…/bundle.zip"
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && submitUrl()}
                />
              </div>
              <button className="btn-ghost shrink-0" onClick={submitUrl} disabled={staging || !url.trim()}>
                Fetch
              </button>
            </div>
          </div>
          {error && <p className="mt-2 text-sm text-danger">{error}</p>}
        </section>

        {/* Store */}
        <section>
          <div className="mb-3 flex items-center gap-2.5">
            <Store className="h-4 w-4 shrink-0 text-fg-muted" />
            <div className="min-w-0 flex-1">
              <h3 className="text-[13px] font-semibold text-fg">Store</h3>
              <p className="text-xs text-fg-faint">Curated, reviewed extensions.</p>
            </div>
            {store.isFetching && !store.isLoading && <Spinner />}
          </div>

          {showControls && (
            <div className="mb-2 flex flex-wrap items-center gap-2">
              <div className="relative min-w-[180px] flex-1">
                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-faint" />
                <input
                  className="input pl-9"
                  placeholder="Search extensions"
                  value={q}
                  onChange={(e) => setQ(e.target.value)}
                />
              </div>
              <div className="seg" role="tablist">
                {(["all", "available", "installed"] as Filter[]).map((f) => (
                  <button
                    key={f}
                    role="tab"
                    aria-selected={filter === f}
                    onClick={() => setFilter(f)}
                    className={clsx("seg-item capitalize", filter === f && "seg-item-active")}
                  >
                    {f}
                  </button>
                ))}
              </div>
              <select
                className="input w-auto"
                value={sort}
                onChange={(e) => setSort(e.target.value as Sort)}
                aria-label="Sort"
              >
                <option value="name">Name</option>
                <option value="author">Author</option>
              </select>
            </div>
          )}

          {store.isLoading ? (
            <div>
              {Array.from({ length: 4 }).map((_, i) => (
                <StoreRowSkeleton key={i} />
              ))}
            </div>
          ) : store.isError ? (
            <p className="rounded-md border border-danger/30 bg-danger/[0.08] px-3 py-2.5 text-sm text-fg">
              Couldn't reach the store. Check the store URL and your connection.
            </p>
          ) : !configured ? (
            <div className="rounded-lg border border-dashed border-line-strong px-4 py-10 text-center">
              <p className="text-sm text-fg-muted">No store is configured.</p>
              <p className="mx-auto mt-1 max-w-md text-xs leading-relaxed text-fg-faint">
                Set a store index URL (the <code className="text-fg-muted">ext.store_url</code> setting) to browse
                curated extensions. You can still install your own above.
              </p>
            </div>
          ) : items.length === 0 ? (
            <p className="rounded-lg border border-dashed border-line-strong px-4 py-10 text-center text-sm text-fg-faint">
              The store is empty right now.
            </p>
          ) : filtered.length === 0 ? (
            <p className="rounded-lg border border-dashed border-line-strong px-4 py-10 text-center text-sm text-fg-faint">
              No extensions match “{q}”.
            </p>
          ) : (
            <div className="overflow-x-auto">
              <table className="tbl">
                <thead>
                  <tr>
                    <th className="w-full">Name</th>
                    <th className="hidden md:table-cell">Author</th>
                    <th className="hidden sm:table-cell">Version</th>
                    <th className="w-px" aria-label="Install" />
                  </tr>
                </thead>
                <tbody>
                  {filtered.map((it) => (
                    <StoreRow key={it.id} item={it} staging={staging} onInstall={() => onStageStore(it.id)} />
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>
    </Modal>
  );
}

function StoreRow({ item, staging, onInstall }: { item: StoreItem; staging: boolean; onInstall: () => void }) {
  return (
    <tr>
      <td className="max-w-0 py-2.5">
        <div className="flex items-center gap-3">
          <ExtIcon name={item.name} size={32} />
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="truncate text-sm font-medium text-fg">{item.name}</span>
              <span className="pill-green shrink-0" title="Reviewed by the owner">
                <ShieldCheck className="h-3 w-3" /> Reviewed
              </span>
            </div>
            <p className="truncate text-xs text-fg-muted" title={item.description}>
              <span className="md:hidden">{item.author ? `by ${item.author}` : "Unknown author"}{item.description ? " · " : ""}</span>
              {item.description}
            </p>
          </div>
        </div>
      </td>
      <td className="hidden whitespace-nowrap md:table-cell">{item.author ? item.author : "Unknown author"}</td>
      <td className="hidden whitespace-nowrap tabular-nums sm:table-cell">v{item.version}</td>
      <td className="whitespace-nowrap text-right">
        {item.installed ? (
          <span className="inline-flex items-center gap-1.5 px-1 text-sm text-fg-muted">
            <Check className="h-4 w-4 text-online" /> Installed
          </span>
        ) : (
          <button className="btn-outline" disabled={staging} onClick={onInstall}>
            Install
          </button>
        )}
      </td>
    </tr>
  );
}

function StoreRowSkeleton() {
  return (
    <div className="flex items-center gap-3 border-b border-line px-3 py-3">
      <Skeleton className="h-8 w-8 rounded-md" />
      <div className="flex-1 space-y-2">
        <Skeleton className="h-3.5 w-1/3" />
        <Skeleton className="h-3 w-1/2" />
      </div>
      <Skeleton className="h-8 w-20" />
    </div>
  );
}
