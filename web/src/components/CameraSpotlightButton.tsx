import { Lightbulb, LightbulbOff } from "lucide-react";
import { useCameraOnvif, useCameraVendor, useSetCameraVendor } from "../hooks/useCameras";

// One-tap spotlight toggle in the player's overlay bar. A light is something you
// flick on while watching, so it gets its own button rather than living behind a
// menu. Rendered only for a camera that actually has one.
export function CameraSpotlightButton({ cameraId, VidBtn }: {
  cameraId: string;
  VidBtn: React.ComponentType<{ onClick: () => void; label: string; active?: boolean; children: React.ReactNode }>;
}) {
  const { data: onvif } = useCameraOnvif(cameraId);
  const { data: vendor } = useCameraVendor(cameraId, !!onvif?.enabled);
  const setVendor = useSetCameraVendor();

  const spot = vendor?.supported ? vendor.spotlight : undefined;
  if (!spot) return null;

  const on = spot.state === 1;
  return (
    <VidBtn
      onClick={() => setVendor.mutate({ id: cameraId, spotlight: { state: on ? 0 : 1 } })}
      label={on ? "Turn the spotlight off" : "Turn the spotlight on"}
      active={on}
    >
      {on ? <Lightbulb className="h-4 w-4" /> : <LightbulbOff className="h-4 w-4" />}
    </VidBtn>
  );
}
