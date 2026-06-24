import { createContext, useCallback, useContext, useState, type ReactNode } from "react";

export interface DownloadItem {
  id: string;
  label: string;
  progress: number; // 0..1, or -1 for indeterminate
  status: "downloading" | "done" | "error";
}

interface DownloadsState {
  items: DownloadItem[];
  start: (url: string, fallbackName: string, label: string) => void;
  dismiss: (id: string) => void;
}

const Ctx = createContext<DownloadsState | null>(null);
let seq = 0;

function parseFilename(cd: string | null): string | null {
  if (!cd) return null;
  const m = /filename="?([^"]+)"?/.exec(cd);
  return m ? m[1] : null;
}

// DownloadsProvider streams authed files to a Blob (so we can show progress),
// then triggers the browser's native save dialog. Progress shows in the
// DownloadsOverlay; items auto-dismiss when finished.
export function DownloadsProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<DownloadItem[]>([]);

  const update = (id: string, patch: Partial<DownloadItem>) =>
    setItems((cur) => cur.map((it) => (it.id === id ? { ...it, ...patch } : it)));
  const dismiss = useCallback(
    (id: string) => setItems((cur) => cur.filter((it) => it.id !== id)),
    [],
  );

  const start = useCallback(
    (url: string, fallbackName: string, label: string) => {
      const id = `dl-${++seq}`;
      setItems((cur) => [...cur, { id, label, progress: -1, status: "downloading" }]);
      void (async () => {
        try {
          const res = await fetch(url, { credentials: "same-origin" });
          if (!res.ok || !res.body) throw new Error(String(res.status));
          const name = parseFilename(res.headers.get("Content-Disposition")) ?? fallbackName;
          const total = Number(res.headers.get("Content-Length")) || 0;
          const reader = res.body.getReader();
          const chunks: Uint8Array[] = [];
          let received = 0;
          for (;;) {
            const { done, value } = await reader.read();
            if (done) break;
            if (value) {
              chunks.push(value);
              received += value.length;
              if (total) update(id, { progress: received / total });
            }
          }
          const blob = new Blob(chunks as BlobPart[], { type: "video/mp4" });
          const obj = URL.createObjectURL(blob);
          const a = document.createElement("a");
          a.href = obj;
          a.download = name;
          document.body.appendChild(a);
          a.click();
          a.remove();
          setTimeout(() => URL.revokeObjectURL(obj), 15000);
          update(id, { progress: 1, status: "done" });
          setTimeout(() => dismiss(id), 4000);
        } catch {
          update(id, { status: "error", progress: 0 });
          setTimeout(() => dismiss(id), 6000);
        }
      })();
    },
    [dismiss],
  );

  return <Ctx.Provider value={{ items, start, dismiss }}>{children}</Ctx.Provider>;
}

export function useDownloads(): DownloadsState {
  const c = useContext(Ctx);
  if (!c) throw new Error("useDownloads must be used within DownloadsProvider");
  return c;
}
