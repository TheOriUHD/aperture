import { useEffect, useRef } from "react";

// Shared Escape-key stack for overlays (modals, dialogs, the marketplace). Only the
// MOST RECENTLY opened overlay responds to Escape, and it consumes the event so
// nothing beneath it — a lower modal, or an app-level key handler — also reacts.
// That gives correct topmost-only dismissal at any stack depth, for this pair and
// any future stacked overlays. Previously each overlay attached its own window
// listener, so Escape fired all of them at once and closed the whole stack.
//
// The stack itself is a plain object (no DOM), so its ordering semantics are
// unit-testable without a browser; the single window listener is a thin wrapper.

type Handler = () => void;

export const escapeStack = {
  handlers: [] as Handler[],
  /** Register a handler; returns an unregister function. */
  push(h: Handler): () => void {
    this.handlers.push(h);
    return () => this.remove(h);
  },
  remove(h: Handler) {
    const i = this.handlers.lastIndexOf(h);
    if (i >= 0) this.handlers.splice(i, 1);
  },
  /** Invoke the topmost handler. Returns true iff one handled it. */
  dispatch(): boolean {
    const top = this.handlers[this.handlers.length - 1];
    if (!top) return false;
    top();
    return true;
  },
};

let listening = false;
function ensureListener() {
  if (listening) return;
  // Bubble phase, so a descendant that legitimately consumes Escape (e.g. Monaco's
  // find widget, an input) can stopPropagation and pre-empt us; an unhandled Escape
  // still bubbles here to close the top overlay.
  window.addEventListener("keydown", (e) => {
    if (e.key !== "Escape") return;
    if (escapeStack.dispatch()) e.stopPropagation();
  });
  listening = true;
}

/** Register `onEscape` while `active`. Only the most recently registered active
 * handler runs on Escape. `onEscape` may change between renders without reordering
 * the stack — a stable wrapper reads the latest value through a ref, so the effect
 * re-runs only when `active` toggles. */
export function useEscapeStack(active: boolean, onEscape: Handler) {
  const ref = useRef(onEscape);
  ref.current = onEscape;
  useEffect(() => {
    if (!active) return;
    ensureListener();
    return escapeStack.push(() => ref.current());
  }, [active]);
}
