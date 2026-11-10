import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import clsx from "clsx";
import { ChevronDown, Search, X } from "lucide-react";
import { useClickOutside } from "../../hooks/useClickOutside";

// SearchSelect picks one item from a long catalogue by typing: a button showing
// the current choice opens a search field over a ranked result list, navigable
// with the arrow keys.
export function SearchSelect<T>({
  value,
  onChange,
  search,
  itemKey,
  renderItem,
  renderValue,
  placeholder,
  total,
  limit = 60,
  onClear,
}: {
  value: T | undefined;
  onChange: (item: T) => void;
  search: (query: string, limit: number) => T[];
  itemKey: (item: T) => string;
  renderItem: (item: T) => ReactNode;
  renderValue: (item: T) => ReactNode;
  placeholder: string;
  total?: number;
  limit?: number;
  onClear?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [cursor, setCursor] = useState(0);
  const ref = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const id = useId();
  useClickOutside(ref, () => setOpen(false), open);

  const results = open ? search(query, limit) : [];
  useEffect(() => setCursor(0), [query, open]);
  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);
  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>(`[data-i="${cursor}"]`)?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  const pick = (item: T) => {
    onChange(item);
    setOpen(false);
    setQuery("");
  };

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={id}
        className={clsx(
          "flex min-h-8 w-full items-center gap-2 rounded-md border bg-white/[0.045] px-3 py-1.5 text-left text-sm transition-colors",
          open ? "border-brand" : "border-white/[0.07] hover:border-white/[0.12]",
        )}
      >
        <span className="min-w-0 flex-1">{value ? renderValue(value) : <span className="text-fg-faint">{placeholder}</span>}</span>
        {value && onClear && (
          <span
            role="button"
            aria-label="Clear"
            onClick={(e) => {
              e.stopPropagation();
              onClear();
            }}
            className="grid h-5 w-5 place-items-center rounded text-fg-faint hover:text-fg"
          >
            <X className="h-3.5 w-3.5" />
          </span>
        )}
        <ChevronDown className={clsx("h-4 w-4 shrink-0 text-fg-faint transition-transform", open && "rotate-180")} />
      </button>
      {open && (
        <div className="absolute left-0 right-0 z-30 mt-1 animate-pop-in overflow-hidden card shadow-pop">
          <div className="relative border-b border-line p-2">
            <Search className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-faint" />
            <input
              ref={inputRef}
              className="input pl-8"
              value={query}
              placeholder="Type a model, e.g. N100, i5-12400, 5800X, Pi 5"
              role="combobox"
              aria-expanded
              aria-controls={id}
              aria-activedescendant={results[cursor] ? `${id}-${cursor}` : undefined}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "ArrowDown") {
                  e.preventDefault();
                  setCursor((c) => Math.min(results.length - 1, c + 1));
                } else if (e.key === "ArrowUp") {
                  e.preventDefault();
                  setCursor((c) => Math.max(0, c - 1));
                } else if (e.key === "Enter") {
                  e.preventDefault();
                  if (results[cursor]) pick(results[cursor]);
                } else if (e.key === "Escape") {
                  e.stopPropagation();
                  setOpen(false);
                }
              }}
            />
          </div>
          <ul ref={listRef} id={id} role="listbox" className="max-h-80 overflow-y-auto py-1">
            {results.length === 0 ? (
              <li className="px-3 py-6 text-center text-sm text-fg-faint">Nothing matches “{query}”.</li>
            ) : (
              results.map((item, i) => (
                <li
                  key={itemKey(item)}
                  id={`${id}-${i}`}
                  data-i={i}
                  role="option"
                  aria-selected={i === cursor}
                  onMouseEnter={() => setCursor(i)}
                  onMouseDown={(e) => {
                    e.preventDefault();
                    pick(item);
                  }}
                  className={clsx("cursor-pointer px-3 py-2", i === cursor ? "bg-white/[0.06]" : "")}
                >
                  {renderItem(item)}
                </li>
              ))
            )}
          </ul>
          {results.length === limit && (
            <div className="border-t border-line px-3 py-1.5 text-[11px] text-fg-faint">
              Showing the first {limit}
              {total ? ` of ${total}` : ""}. Keep typing to narrow it down.
            </div>
          )}
        </div>
      )}
    </div>
  );
}
