import { useEffect, useRef, useState } from "react";
import { X, Eraser, Brush, RotateCcw } from "lucide-react";
import { api } from "../lib/api";

const COLS = 32;
const ROWS = 18;

export function ZoneEditor({
  cameraId,
  cameraName,
  onClose,
}: {
  cameraId: string;
  cameraName: string;
  onClose: () => void;
}) {
  const [ignore, setIgnore] = useState<Set<number>>(new Set());
  const [tool, setTool] = useState<"ignore" | "watch">("ignore");
  const [saving, setSaving] = useState(false);
  const painting = useRef(false);
  const snapUrl = useRef(`/api/cameras/${cameraId}/snapshot.jpg?t=${Date.now()}`);

  useEffect(() => {
    void api
      .get<{ cols: number; rows: number; ignore: number[] }>(`/api/cameras/${cameraId}/zones`)
      .then((m) => {
        if (!m || !m.cols || !m.rows || m.ignore.length === 0) return;
        const next = new Set<number>();
        for (const idx of m.ignore) {
          const lc = idx % m.cols;
          const lr = Math.floor(idx / m.cols);
          const ec = Math.floor((lc * COLS) / m.cols);
          const er = Math.floor((lr * ROWS) / m.rows);
          next.add(er * COLS + ec);
        }
        setIgnore(next);
      })
      .catch(() => {});
  }, [cameraId]);

  useEffect(() => {
    const up = () => (painting.current = false);
    window.addEventListener("mouseup", up);
    window.addEventListener("touchend", up);
    return () => {
      window.removeEventListener("mouseup", up);
      window.removeEventListener("touchend", up);
    };
  }, []);

  function paint(i: number) {
    setIgnore((prev) => {
      const n = new Set(prev);
      if (tool === "ignore") n.add(i);
      else n.delete(i);
      return n;
    });
  }

  function onTouchMove(e: React.TouchEvent) {
    if (!painting.current) return;
    const t = e.touches[0];
    const el = document.elementFromPoint(t.clientX, t.clientY) as HTMLElement | null;
    const idx = el?.dataset.cell;
    if (idx != null) paint(Number(idx));
  }

  async function save() {
    setSaving(true);
    try {
      await api.put(`/api/cameras/${cameraId}/zones`, { cols: COLS, rows: ROWS, ignore: [...ignore] });
      onClose();
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="fixed inset-0 z-[55] grid place-items-center p-4">
      <div className="absolute inset-0 bg-black/70" onClick={onClose} />
      <div className="relative w-full max-w-2xl card shadow-pop animate-fade-in">
        <div className="flex items-center justify-between px-5 h-14 border-b border-line">
          <h2 className="text-base font-medium text-fg">Motion zones · {cameraName}</h2>
          <button className="btn-icon" onClick={onClose} aria-label="Close">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="p-5 space-y-3">
          <p className="text-xs text-fg-faint leading-relaxed">
            Paint over areas to <span className="text-fg-muted">ignore</span> (a swaying tree, a public
            street). Motion there won’t trigger events. Drag to paint.
          </p>

          <div
            className="relative w-full aspect-video rounded-lg overflow-hidden bg-ink-950 select-none touch-none"
            onTouchMove={onTouchMove}
          >
            <img
              src={snapUrl.current}
              alt=""
              className="absolute inset-0 w-full h-full object-cover"
              draggable={false}
            />
            <div
              className="absolute inset-0 grid"
              style={{ gridTemplateColumns: `repeat(${COLS},1fr)`, gridTemplateRows: `repeat(${ROWS},1fr)` }}
            >
              {Array.from({ length: COLS * ROWS }, (_, i) => {
                const on = ignore.has(i);
                return (
                  <div
                    key={i}
                    data-cell={i}
                    onMouseDown={() => {
                      painting.current = true;
                      paint(i);
                    }}
                    onMouseEnter={() => painting.current && paint(i)}
                    onTouchStart={() => {
                      painting.current = true;
                      paint(i);
                    }}
                    className={`border-[0.5px] border-white/5 ${
                      on ? "bg-rec/45" : "hover:bg-white/10"
                    }`}
                  />
                );
              })}
            </div>
          </div>

          <div className="flex items-center gap-2">
            <button
              className={`btn h-8 text-xs ${tool === "ignore" ? "bg-brand/15 text-brand border border-brand/40" : "btn-subtle"}`}
              onClick={() => setTool("ignore")}
            >
              <Brush className="h-3.5 w-3.5" /> Ignore
            </button>
            <button
              className={`btn h-8 text-xs ${tool === "watch" ? "bg-brand/15 text-brand border border-brand/40" : "btn-subtle"}`}
              onClick={() => setTool("watch")}
            >
              <Eraser className="h-3.5 w-3.5" /> Erase
            </button>
            <button className="btn-subtle h-8 text-xs" onClick={() => setIgnore(new Set())}>
              <RotateCcw className="h-3.5 w-3.5" /> Clear
            </button>
            <span className="ml-auto text-xs text-fg-faint">{ignore.size} cells ignored</span>
          </div>
        </div>

        <div className="flex justify-end gap-2 px-5 py-4 border-t border-line">
          <button className="btn-ghost" onClick={onClose}>
            Cancel
          </button>
          <button className="btn-primary" onClick={save} disabled={saving}>
            {saving ? "Saving…" : "Save zones"}
          </button>
        </div>
      </div>
    </div>
  );
}
