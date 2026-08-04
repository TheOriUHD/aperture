import { type CSSProperties } from "react";
import { Minimize2 } from "lucide-react";

// Mobile pseudo-fullscreen. iPhone blocks element requestFullscreen, so instead
// we turn the <video> a quarter turn and blow it up to fill the screen. A
// 100vh×100vw box, centred and rotated 90°, lands exactly edge-to-edge in a
// portrait viewport. The style is applied to the <video> ELEMENT itself (not an
// ancestor) so iOS — which composites playing video in its own layer — honours
// the transform.
export const fsVideoStyle: CSSProperties = {
  position: "fixed",
  top: "50%",
  left: "50%",
  width: "100vh",
  height: "100vw",
  // Tailwind's preflight sets `video { max-width: 100%; height: auto }`, which
  // would clamp width:100vh back to the viewport width and shrink the video to a
  // small centred box. Undo that so the 100vh×100vw box (and thus the fill) holds.
  maxWidth: "none",
  maxHeight: "none",
  transform: "translate(-50%, -50%) rotate(90deg)",
  WebkitTransform: "translate(-50%, -50%) rotate(90deg)",
  objectFit: "contain",
  background: "#000",
  zIndex: 9999,
};

// Black backdrop (tap to exit) + an exit button, layered around the rotated
// video during mobile pseudo-fullscreen.
export function FullscreenChrome({ onExit }: { onExit: () => void }) {
  return (
    <>
      <div className="fixed inset-0 bg-black" style={{ zIndex: 9998 }} onClick={onExit} />
      <button
        onClick={onExit}
        className="fixed grid h-10 w-10 place-items-center rounded-full bg-black/55 text-white"
        style={{
          zIndex: 10000,
          // Clear the iOS status bar / notch — top-3 alone tucks under it.
          top: "calc(env(safe-area-inset-top) + 0.5rem)",
          right: "calc(env(safe-area-inset-right) + 0.75rem)",
        }}
        aria-label="Exit fullscreen"
      >
        <Minimize2 className="h-5 w-5" />
      </button>
    </>
  );
}
