import { useEffect, useRef, useState } from "react";
import { OBJECT_TTL, useLiveCamera } from "../lib/liveState";
import { ObjectBox, useHoveredBox, useObjectBoxes } from "./ObjectBox";
import { useContentRect } from "./useContentRect";

// LiveObjectsOverlay draws, over a live picture, a white outline round the
// frame while the camera sees motion, and a labelled box round each object
// recognised in it. Boxes glide briefly (at most 150 ms) from one recognised
// position to the next: smooth, without adding much delay behind the picture. They obey the "Show object boxes" switch; the outline always shows.
export function LiveObjectsOverlay({
  cameraId,
  videoRef,
  fit = "cover",
  outline = true,
}: {
  cameraId: string;
  videoRef: React.RefObject<HTMLVideoElement>;
  fit?: "cover" | "contain";
  outline?: boolean;
}) {
  const live = useLiveCamera(cameraId);
  const [boxesOn] = useObjectBoxes();
  const hostRef = useRef<HTMLDivElement>(null);
  const rect = useContentRect(hostRef, videoRef, fit);
  const [, setTick] = useState(0);

  // How long between updates for this camera: the glide time.
  const lastAt = useRef(0);
  const gap = useRef(320);
  useEffect(() => {
    if (!live.objectsAt) return;
    if (lastAt.current) {
      const d = live.objectsAt - lastAt.current;
      if (d > 60 && d < 1500) gap.current = gap.current * 0.7 + d * 0.3;
    }
    lastAt.current = live.objectsAt;
  }, [live.objectsAt]);

  // Boxes go stale on their own: re-render once they expire.
  const age = Date.now() - live.objectsAt;
  const showBoxes = boxesOn && live.objects.length > 0 && age < OBJECT_TTL;
  const keyed = showBoxes ? live.objects.map((o, i) => ({ ...o, key: String(o.id ?? `${o.label}-${i}`) })) : [];
  const hovered = useHoveredBox(hostRef, rect, keyed);
  useEffect(() => {
    if (!showBoxes) return;
    const t = window.setTimeout(() => setTick((n) => n + 1), OBJECT_TTL - age + 20);
    return () => window.clearTimeout(t);
  }, [showBoxes, age, live.objectsAt]);

  return (
    <div ref={hostRef} className="pointer-events-none absolute inset-0 z-[5] overflow-hidden">
      {outline && (
        <div
          className={`absolute inset-0 rounded-[inherit] shadow-[inset_0_0_0_2px_rgba(255,255,255,0.95),inset_0_0_16px_rgba(255,255,255,0.22)] transition-opacity duration-300 ${
            live.moving ? "opacity-100" : "opacity-0"
          }`}
        />
      )}
      {showBoxes && (
        <div className="absolute" style={rect}>
          {keyed.map(({ key, ...o }) => (
            <ObjectBox
              key={key}
              {...o}
              expanded={key === hovered}
              glide={Math.round(Math.min(gap.current * 0.5, 150))}
            />
          ))}
        </div>
      )}
    </div>
  );
}
