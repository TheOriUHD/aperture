import { useEffect, useState } from "react";

export type ContentRect = { left: number; top: number; width: number; height: number };

// useContentRect is where the picture actually is inside its box: a video that
// fills its tile ("cover") overflows it, one shown whole ("contain") leaves
// bars. Overlays position themselves in this rect so normalised coordinates
// land on the right pixels. Until the video knows its size, 16:9 is assumed.
export function useContentRect(
  hostRef: React.RefObject<HTMLElement>,
  videoRef: React.RefObject<HTMLVideoElement>,
  fit: "cover" | "contain",
): ContentRect {
  const [rect, setRect] = useState<ContentRect>({ left: 0, top: 0, width: 0, height: 0 });
  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const measure = () => {
      const cw = host.clientWidth;
      const ch = host.clientHeight;
      const vid = videoRef.current;
      const vw = vid?.videoWidth || 16;
      const vh = vid?.videoHeight || 9;
      const scale = fit === "cover" ? Math.max(cw / vw, ch / vh) : Math.min(cw / vw, ch / vh);
      const w = vw * scale;
      const h = vh * scale;
      setRect({ left: (cw - w) / 2, top: (ch - h) / 2, width: w, height: h });
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(host);
    const v = videoRef.current;
    v?.addEventListener("loadedmetadata", measure);
    v?.addEventListener("resize", measure);
    // The video element can arrive after the overlay mounts.
    const late = window.setTimeout(measure, 1500);
    return () => {
      ro.disconnect();
      v?.removeEventListener("loadedmetadata", measure);
      v?.removeEventListener("resize", measure);
      window.clearTimeout(late);
    };
  }, [hostRef, videoRef, fit]);
  return rect;
}
