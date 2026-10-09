import { useEffect, useState } from "react";
import { Lightbulb, LightbulbOff } from "lucide-react";
import { useCameraOnvif, useCameraVendor, useCameras, useSetCameraVendor } from "../hooks/useCameras";

// How long a refusal stays over the picture: long enough to read a sentence, short
// enough that it does not linger.
const REFUSAL_MS = 8000;

// One-tap spotlight toggle in the player's overlay bar. A light is something you
// flick on while watching, so it gets its own button rather than living behind a
// menu. Rendered only for a camera that actually has one.
export function CameraSpotlightButton({ cameraId, VidBtn }: {
  cameraId: string;
  VidBtn: React.ComponentType<{ onClick: () => void; label: string; active?: boolean; children: React.ReactNode }>;
}) {
  const { data: onvif } = useCameraOnvif(cameraId);
  const { data: cameras } = useCameras();
  // Marking the camera as a Reolink is enough on its own. Gating the lookup on ONVIF
  // being enabled was the other half of why this button kept disappearing: the vendor
  // controls do not go through ONVIF at all — they use the camera's own HTTP API — so
  // an ONVIF toggle should never decide whether they show.
  const marked = cameras?.find((c) => c.id === cameraId)?.vendor === "reolink";
  const { data: vendor } = useCameraVendor(cameraId, marked || !!onvif?.enabled);
  const setVendor = useSetCameraVendor();

  // The camera's reason when it refuses. Without it a refused switch just flips back
  // on its own, which reads as a broken button rather than as the camera saying no.
  const [refusal, setRefusal] = useState("");
  useEffect(() => {
    if (!refusal) return;
    const t = window.setTimeout(() => setRefusal(""), REFUSAL_MS);
    return () => window.clearTimeout(t);
  }, [refusal]);
  useEffect(() => setRefusal(""), [cameraId]);

  const spot = vendor?.supported ? vendor.spotlight : undefined;
  if (!spot) return null;

  const on = spot.state === 1;
  return (
    <span className="relative inline-flex">
      <VidBtn
        onClick={() => {
          setRefusal("");
          setVendor.mutate(
            { id: cameraId, spotlight: { state: on ? 0 : 1 } },
            { onError: (e) => setRefusal(e instanceof Error ? e.message : "The camera refused the change.") },
          );
        }}
        label={on ? "Turn the spotlight off" : "Turn the spotlight on"}
        active={on}
      >
        {on ? <Lightbulb className="h-4 w-4" /> : <LightbulbOff className="h-4 w-4" />}
      </VidBtn>
      {refusal && (
        <span
          role="alert"
          className="absolute right-0 top-full z-20 mt-2 w-64 rounded-md border border-rec/40 bg-black/85 px-2.5 py-2 text-left text-[11px] leading-snug text-white backdrop-blur-sm"
        >
          {refusal}
        </span>
      )}
    </span>
  );
}
