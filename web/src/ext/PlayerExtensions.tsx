import { useRef, useState } from "react";
import { useClickOutside } from "../hooks/useClickOutside";
import { ExtensionPanel } from "./ExtensionSlot";
import { panelsForSlot, type ExtPanel } from "./panels";
import { useExtPanels } from "./useExtPanels";

// Extension UI that lives ON the player rather than under it.
//
// Both components render NOTHING when no extension contributes to their slot, which
// is what keeps every player pixel-identical with zero extensions installed — the
// same guarantee ExtensionSlot gives the card slots, and pinned by panels.test.ts.
//
// Both are camera-context, so panelsForSlot applies the extension's camera grant:
// an extension granted two cameras gets its button and its widget on exactly those
// two players and nowhere else.

/**
 * ExtensionToolbar renders one icon button per `player-toolbar` panel into the
 * player's top-right control cluster, each owning a dropdown that hosts the
 * extension's own panel.
 *
 * The button is `.osd-btn` and the dropdown `.osd-panel` — the same classes
 * first-party controls use — so an extension entry is visually indistinguishable
 * from the picture-settings or camera-settings buttons beside it instead of
 * approximating them with its own colours.
 */
export function ExtensionToolbar({ cameraId }: { cameraId?: string }) {
  const { data: panels = [] } = useExtPanels();
  const forSlot = panelsForSlot(panels, "player-toolbar", cameraId);
  if (forSlot.length === 0) return null;
  return (
    <>
      {forSlot.map((p) => (
        <ToolbarEntry key={p.extId + ":" + p.entry} panel={p} cameraId={cameraId} />
      ))}
    </>
  );
}

function ToolbarEntry({ panel, cameraId }: { panel: ExtPanel; cameraId?: string }) {
  const [open, setOpen] = useState(false);
  const wrap = useRef<HTMLDivElement>(null);
  // A document-level listener rather than a full-screen catcher: every button in
  // this cluster has backdrop-filter, which on WebKit clips a position:fixed
  // catcher to that ancestor's box and makes it miss most of the screen.
  useClickOutside(wrap, () => setOpen(false), open);

  const label = panel.title || panel.extName;
  const letter = (panel.extName.trim()[0] || "?").toUpperCase();

  return (
    <div className="relative" ref={wrap}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-label={label}
        title={label}
        aria-expanded={open}
        className={`osd-btn ${open ? "osd-btn-active" : ""}`}
      >
        {panel.icon ? (
          // Served as an <img> from the extension's CSP'd asset route — never
          // inlined as markup, so a hostile bundle cannot run script in the
          // user's session by way of its icon.
          <img src={panel.icon} alt="" className="h-4 w-4 rounded-sm object-cover" />
        ) : (
          <span className="text-[11px] font-semibold leading-none">{letter}</span>
        )}
      </button>
      {open && (
        // Anchored under the button like the picture-settings popover. Note the
        // player container is overflow-hidden, so this is clipped to the video box
        // by design — a dropdown cannot escape the picture.
        <div className="osd-panel absolute right-0 top-10 z-30 w-72 overflow-hidden">
          <div className="flex items-center justify-between px-3 pt-2.5">
            <span className="text-xs font-medium text-white/90">{label}</span>
            <button
              type="button"
              onClick={() => setOpen(false)}
              className="text-white/50 hover:text-white"
              aria-label="Close"
            >
              ×
            </button>
          </div>
          <ExtensionPanel panel={panel} cameraId={cameraId} chrome="bare" initialHeight={160} />
        </div>
      )}
    </div>
  );
}

// Corner classes for an OSD widget, inset from the edges by the same amount
// first-party chrome uses (top-3 / right-3 / bottom-3).
//
// bottom-left is offset horizontally rather than sitting at left-3, because the
// player's own keyboard-shortcuts button lives exactly there (Playback's
// KeyboardHelp: absolute bottom-3 left-3, h-8 w-8). At left-3 an OSD widget lands
// ON TOP of it and hides it. The offset clears that button and lines the widget up
// beside it, so extension controls read as part of the same bottom-left cluster
// instead of covering first-party chrome.
const FIRST_PARTY_BOTTOM_LEFT = "left-[3.25rem]"; // 12px inset + 32px button + 8px gap
const ANCHOR_CLASS: Record<string, string> = {
  "top-left": "top-3 left-3",
  "top-right": "top-3 right-3",
  "bottom-left": `bottom-3 ${FIRST_PARTY_BOTTOM_LEFT}`,
  "bottom-right": "bottom-3 right-3",
};

/**
 * ExtensionOSD renders `player-osd` widgets over the picture.
 *
 * The host owns placement; the panel owns its content and asks for a height via the
 * SDK's resize(). Widgets sharing a corner stack in declaration order.
 *
 * pointer-events are enabled on the widget but the container is inert, so an OSD
 * never swallows a click meant for the video, the scrub gesture, or the controls
 * beneath it.
 */
export function ExtensionOSD({ cameraId }: { cameraId?: string }) {
  const { data: panels = [] } = useExtPanels();
  const forSlot = panelsForSlot(panels, "player-osd", cameraId);
  if (forSlot.length === 0) return null;

  const byAnchor = new Map<string, ExtPanel[]>();
  for (const p of forSlot) {
    const a = p.anchor && ANCHOR_CLASS[p.anchor] ? p.anchor : "bottom-left";
    byAnchor.set(a, [...(byAnchor.get(a) ?? []), p]);
  }

  return (
    <div className="pointer-events-none absolute inset-0 z-[25]">
      {[...byAnchor.entries()].map(([anchor, list]) => (
        <div
          key={anchor}
          className={`pointer-events-none absolute flex flex-col items-start gap-2 ${ANCHOR_CLASS[anchor]}`}
        >
          {list.map((p) => (
            // w-max so the wrapper shrink-wraps whatever width the panel negotiated.
            // A fixed width here would keep a collapsed widget's dead area over the
            // video, catching clicks meant for the picture.
            <div key={p.extId + ":" + p.entry} className="pointer-events-auto w-max overflow-hidden rounded-lg">
              <ExtensionPanel
                panel={p}
                cameraId={cameraId}
                chrome="bare"
                width={168}
                initialHeight={168}
              />
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}
