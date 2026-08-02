import { useEffect } from "react";

let lockCount = 0;

// Freezes the app's scrollable screen containers while a modal is open, so the
// background doesn't scroll under it (notably on mobile). Ref-counted so stacked
// modals don't unlock early. Sets `data-modal-open` on <html>; index.css freezes
// `.overflow-auto` (the per-screen scroll containers) while it's present. The
// modal's own card uses `overflow-y-auto`, which is deliberately not frozen.
export function useScrollLock(active: boolean) {
  useEffect(() => {
    if (!active) return;
    lockCount++;
    document.documentElement.setAttribute("data-modal-open", "");
    return () => {
      lockCount = Math.max(0, lockCount - 1);
      if (lockCount === 0) document.documentElement.removeAttribute("data-modal-open");
    };
  }, [active]);
}
