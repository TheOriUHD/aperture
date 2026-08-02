import { useEffect, type RefObject } from "react";

// Calls `onOutside` when a pointer-down lands outside `ref` (while `active`).
// Uses a document-level listener rather than a full-screen catcher element, so
// it works even when an ancestor has `backdrop-filter` (which would otherwise
// clip a `position:fixed` catcher to that ancestor's box on WebKit).
export function useClickOutside(
  ref: RefObject<HTMLElement | null>,
  onOutside: () => void,
  active: boolean,
) {
  useEffect(() => {
    if (!active) return;
    const handler = (e: PointerEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onOutside();
    };
    document.addEventListener("pointerdown", handler, true);
    return () => document.removeEventListener("pointerdown", handler, true);
  }, [ref, onOutside, active]);
}
