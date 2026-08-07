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

// Marketplace — the "+" surface. A premium overlay (the shared Modal at its widest)
// with "install your own" up top and an instant-search store grid below. It only
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
    <Modal open={open} onClose={onClose} title="Add extensions" size="2xl">
      <div className="space-y-5">
        {/* Install your own — real presence, but compact and above the store. */}
        <div className="rounded-xl border border-line bg-ink-850 p-4">
          <p className="text-xs font-medium text-fg-muted">Install your own</p>
          <p className="mt-0.5 text-[11px] text-fg-faint">From a bundle file, or a URL you trust. Sideloaded extensions carry a warning.</p>
          <div className="mt-3 flex flex-col gap-2 sm:flex-row">
            <button className="btn-subtle h-9 sm:w-40" onClick={() => fileRef.current?.click()} disabled={staging}>
              <Upload className="h-4 w-4" /> {staging ? "Reading…" : "Upload file"}
            </button>
            <input ref={fileRef} type="file" accept=".zip" hidden onChange={onFile} />
            <div className="flex min-w-0 flex-1 gap-2">
              <div className="relative min-w-0 flex-1">
                <Link2 className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-fg-faint" />
                <input
                  className="input pl-8"
                  placeholder="https://…/bundle.zip"
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && submitUrl()}
                />
              </div>
              <button className="btn-subtle h-9 shrink-0" onClick={submitUrl} disabled={staging || !url.trim()}>
                Fetch
              </button>
            </div>
          </div>
          {error && <p className="mt-2 text-sm text-rec">{error}</p>}
        </div>

        {/* Store */}
        <div>
          <div className="mb-3 flex items-center gap-2">
            <Store className="h-4 w-4 text-fg-muted" />
            <div className="flex-1">
              <h3 className="text-sm font-medium text-fg">Store</h3>
              <p className="text-[11px] text-fg-faint">Curated, reviewed extensions.</p>
            </div>
            {store.isFetching && !store.isLoading && <Spinner />}
          </div>

          {showControls && (
            <div className="mb-3 flex flex-wrap items-center gap-2">
              <div className="relative min-w-[160px] flex-1">
                <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-fg-faint" />
                <input
                  className="input h-8 pl-8 text-xs"
                  placeholder="Search extensions"
                  value={q}
                  onChange={(e) => setQ(e.target.value)}
                />
              </div>
              <div className="flex gap-1.5">
                {(["all", "available", "installed"] as Filter[]).map((f) => (
                  <button
                    key={f}
                    onClick={() => setFilter(f)}
                    className={clsx(
                      "rounded-full px-3 py-1 text-xs capitalize transition-colors",
                      filter === f ? "bg-brand text-white" : "bg-ink-700 text-fg-muted hover:bg-ink-650 hover:text-fg",
                    )}
                  >
                    {f}
                  </button>
                ))}
              </div>
              <select
                className="input h-8 w-auto text-xs"
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
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {Array.from({ length: 6 }).map((_, i) => (
                <StoreCardSkeleton key={i} />
              ))}
            </div>
          ) : store.isError ? (
            <p className="rounded-lg border border-rec/30 bg-rec/10 px-3 py-2.5 text-sm text-fg">
              Couldn't reach the store. Check the store URL and your connection.
            </p>
          ) : !configured ? (
            <div className="rounded-xl border border-dashed border-line px-3 py-10 text-center">
              <p className="text-sm text-fg-muted">No store is configured.</p>
              <p className="mt-1 text-xs text-fg-faint">
                Set a store index URL (the <code className="text-fg-muted">ext.store_url</code> setting) to browse
                curated extensions. You can still install your own above.
              </p>
            </div>
          ) : items.length === 0 ? (
            <p className="rounded-xl border border-dashed border-line px-3 py-10 text-center text-sm text-fg-faint">
              The store is empty right now.
            </p>
          ) : filtered.length === 0 ? (
            <p className="rounded-xl border border-dashed border-line px-3 py-10 text-center text-sm text-fg-faint">
              No extensions match “{q}”.
            </p>
          ) : (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {filtered.map((it) => (
                <StoreCard key={it.id} item={it} staging={staging} onInstall={() => onStageStore(it.id)} />
              ))}
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
}

function StoreCard({ item, staging, onInstall }: { item: StoreItem; staging: boolean; onInstall: () => void }) {
  return (
    <div className="flex flex-col gap-3 rounded-xl border border-line bg-ink-850 p-3.5 transition-colors hover:border-line-strong">
      <div className="flex items-start gap-3">
        <ExtIcon name={item.name} size={44} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="truncate text-sm font-medium text-fg">{item.name}</span>
            <span className="shrink-0 text-xs tabular-nums text-fg-faint">v{item.version}</span>
          </div>
          <p className="truncate text-xs text-fg-muted">{item.author ? `by ${item.author}` : "Unknown author"}</p>
        </div>
        <span className="pill shrink-0 bg-online/15 text-online" title="Reviewed by the owner">
          <ShieldCheck className="h-3 w-3" />
        </span>
      </div>
      <p className="line-clamp-2 min-h-[2rem] text-xs text-fg-muted">{item.description || " "}</p>
      <div className="mt-auto">
        {item.installed ? (
          <span className="inline-flex items-center gap-1.5 text-xs text-fg-faint">
            <Check className="h-3.5 w-3.5 text-online" /> Installed
          </span>
        ) : (
          <button className="btn-subtle h-8 w-full" disabled={staging} onClick={onInstall}>
            Install
          </button>
        )}
      </div>
    </div>
  );
}

function StoreCardSkeleton() {
  return (
    <div className="flex flex-col gap-3 rounded-xl border border-line bg-ink-850 p-3.5">
      <div className="flex items-start gap-3">
        <Skeleton className="h-11 w-11 rounded-lg" />
        <div className="flex-1 space-y-2">
          <Skeleton className="h-3.5 w-2/3" />
          <Skeleton className="h-3 w-1/3" />
        </div>
      </div>
      <Skeleton className="h-8 w-full" />
      <Skeleton className="h-8 w-full" />
    </div>
  );
}
