import { Store, ShieldCheck, Check } from "lucide-react";
import { Spinner } from "../components/ui/Spinner";
import { useExtStore } from "./useExtensions";
import type { StoreItem } from "./types";

// The curated store, a distinct section inside the manager (not a separate screen).
// Store items install through the identical stage → consent → confirm path as a
// sideload, but carry source=store, so the consent modal shows the reviewed badge
// instead of the sideload warning.
export function StoreSection({ onInstall, busy }: { onInstall: (id: string) => void; busy: boolean }) {
  const q = useExtStore(true);
  return (
    <div className="panel">
      <div className="mb-3 flex items-center gap-2">
        <Store className="h-4 w-4 text-fg-muted" />
        <div className="flex-1">
          <h2 className="text-sm font-medium text-fg">Store</h2>
          <p className="text-xs text-fg-muted">Curated extensions, reviewed and republished by the owner.</p>
        </div>
        {q.isFetching && !q.isLoading && <Spinner />}
      </div>

      {q.isLoading ? (
        <div className="grid place-items-center py-8">
          <Spinner label="Loading store" />
        </div>
      ) : q.isError ? (
        <p className="rounded-lg border border-rec/30 bg-rec/10 px-3 py-2.5 text-sm text-fg">
          Couldn't reach the store. Check the store URL and your connection.
        </p>
      ) : !q.data?.configured ? (
        <div className="rounded-lg border border-dashed border-line px-3 py-8 text-center">
          <p className="text-sm text-fg-muted">No store is configured.</p>
          <p className="mt-1 text-xs text-fg-faint">
            Set a store index URL (the <code className="text-fg-muted">ext.store_url</code> setting) to browse curated
            extensions here.
          </p>
        </div>
      ) : !q.data.extensions.length ? (
        <p className="rounded-lg border border-dashed border-line px-3 py-8 text-center text-sm text-fg-faint">
          The store is empty right now.
        </p>
      ) : (
        <div className="grid gap-2 sm:grid-cols-2">
          {q.data.extensions.map((it) => (
            <StoreCard key={it.id} item={it} busy={busy} onInstall={() => onInstall(it.id)} />
          ))}
        </div>
      )}
    </div>
  );
}

function StoreCard({ item, busy, onInstall }: { item: StoreItem; busy: boolean; onInstall: () => void }) {
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-line bg-ink-850 p-3">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-1.5">
            <span className="truncate font-medium text-fg">{item.name}</span>
            <span className="pill bg-ink-700 text-fg-muted">v{item.version}</span>
          </div>
          <p className="text-xs text-fg-muted">{item.author ? `by ${item.author}` : "Unknown author"}</p>
        </div>
        <span className="pill shrink-0 bg-online/15 text-online" title="Reviewed by the owner">
          <ShieldCheck className="h-3 w-3" />
        </span>
      </div>
      {item.description && <p className="line-clamp-2 text-xs text-fg-muted">{item.description}</p>}
      <div className="mt-auto pt-1">
        {item.installed ? (
          <span className="inline-flex items-center gap-1.5 text-xs text-fg-faint">
            <Check className="h-3.5 w-3.5 text-online" /> Installed
          </span>
        ) : (
          <button className="btn-subtle h-8 w-full" disabled={busy} onClick={onInstall}>
            Install
          </button>
        )}
      </div>
    </div>
  );
}
