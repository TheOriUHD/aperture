import { useEffect, useState } from "react";
import { SlidersHorizontal, X, Check, Loader2, Lightbulb } from "lucide-react";
import {
  useCameraOnvif,
  useSetCameraImaging,
  useCameras,
  useCameraVendor,
  useSetCameraVendor,
  type OnvifImaging,
} from "../hooks/useCameras";

// On-video picture controls. The button only exists when this camera's ONVIF is
// switched on AND actually answering with adjustable settings — so it never
// appears as a dead control on a camera that can't do it.
export function CameraImagingOverlay({ cameraId, VidBtn }: {
  cameraId: string;
  VidBtn: React.ComponentType<{ onClick: () => void; label: string; active?: boolean; children: React.ReactNode }>;
}) {
  const { data } = useCameraOnvif(cameraId);
  const { data: cameras } = useCameras();
  const setImg = useSetCameraImaging();
  // Vendor extras (Reolink spotlight/IR). These do NOT go through ONVIF — they use
  // the camera's own HTTP API — so an explicit vendor marking is enough on its own.
  // ONVIF being on still fetches them, for a camera identified by probe alone.
  const marked = cameras?.find((c) => c.id === cameraId)?.vendor === "reolink";
  const { data: vendor } = useCameraVendor(cameraId, marked || !!data?.enabled);
  const setVendor = useSetCameraVendor();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<OnvifImaging | null>(null);
  const [err, setErr] = useState("");
  const [ok, setOk] = useState(false);

  useEffect(() => {
    if (data?.imaging) setDraft(data.imaging);
  }, [data]);

  // Nothing to offer → no button at all.
  const hasVendor = !!vendor?.supported && !!vendor.spotlight;
  if (!data?.enabled || (!draft && !hasVendor)) return null;

  async function apply(patch: Partial<OnvifImaging>) {
    setErr("");
    setOk(false);
    try {
      await setImg.mutateAsync({ id: cameraId, ...patch });
      setOk(true);
      setTimeout(() => setOk(false), 1200);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "The camera rejected that.");
    }
  }

  const rows: { key: keyof OnvifImaging; label: string }[] = [
    { key: "brightness", label: "Brightness" },
    { key: "contrast", label: "Contrast" },
    { key: "saturation", label: "Saturation" },
    { key: "sharpness", label: "Sharpness" },
  ];

  return (
    <>
      <VidBtn onClick={() => setOpen((v) => !v)} label="Picture settings" active={open}>
        <SlidersHorizontal className="h-4 w-4" />
      </VidBtn>

      {open && (
        <div className="osd-panel absolute right-0 top-10 z-20 w-64 p-3">
          <div className="mb-2.5 flex items-center justify-between">
            <span className="text-[13px] font-semibold text-white">Picture</span>
            <div className="flex items-center gap-1.5">
              {setImg.isPending && <Loader2 className="h-3 w-3 animate-spin text-white/60" />}
              {ok && <Check className="h-3.5 w-3.5 text-online" />}
              <button
                onClick={() => setOpen(false)}
                className="grid h-6 w-6 place-items-center rounded text-white/50 transition-colors hover:bg-white/10 hover:text-white"
                aria-label="Close"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>

          <div className="space-y-2.5">
            {draft && rows.map(({ key, label }) => {
              const val = draft[key] as number | undefined;
              if (val == null) return null;
              const r = data.imaging?.ranges?.[key] ?? { min: 0, max: 255 };
              return (
                <div key={key}>
                  <div className="flex items-center justify-between">
                    <span className="text-xs text-white/75">{label}</span>
                    <span className="text-xs tabular-nums text-white/50">{Math.round(val)}</span>
                  </div>
                  <input
                    type="range"
                    className="w-full accent-brand"
                    min={r.min}
                    max={r.max}
                    value={val}
                    disabled={setImg.isPending}
                    onChange={(e) => setDraft((d) => (d ? { ...d, [key]: Number(e.target.value) } : d))}
                    onMouseUp={() => apply({ [key]: val } as Partial<OnvifImaging>)}
                    onTouchEnd={() => apply({ [key]: val } as Partial<OnvifImaging>)}
                  />
                </div>
              );
            })}

            {draft?.irCutFilter && (
              <div>
                <span className="text-xs text-white/75">Day / night</span>
                <div className="mt-1.5 flex overflow-hidden rounded-md border border-white/10">
                  {(draft?.irCutOptions?.length ? draft.irCutOptions : ["ON", "OFF", "AUTO"]).map((m) => (
                    <button
                      key={m}
                      disabled={setImg.isPending}
                      onClick={() => {
                        setDraft((d) => (d ? { ...d, irCutFilter: m } : d));
                        apply({ irCutFilter: m });
                      }}
                      className={`h-7 flex-1 px-1.5 text-xs transition-colors [&+&]:border-l [&+&]:border-white/10 ${
                        draft?.irCutFilter === m ? "bg-brand/15 font-medium text-brand" : "text-white/70 hover:bg-white/[0.06] hover:text-white"
                      }`}
                    >
                      {m === "ON" ? "Day" : m === "OFF" ? "Night" : "Auto"}
                    </button>
                  ))}
                </div>
              </div>
            )}
          </div>

          {hasVendor && vendor?.spotlight && (
            <div className="mt-3 border-t border-white/10 pt-3">
              {/* On/off lives in the overlay bar as its own button — this panel
                  keeps the settings you tune rather than flick. */}
              <span className="flex items-center gap-1.5 text-xs font-medium text-white/90">
                <Lightbulb className="h-3.5 w-3.5 text-warn" /> Spotlight
              </span>
              <div className="mt-1.5">
                <div className="flex items-center justify-between">
                  <span className="text-xs text-white/75">Brightness</span>
                  <span className="text-xs tabular-nums text-white/50">{vendor.spotlight.bright}</span>
                </div>
                <input
                  type="range"
                  className="w-full accent-warn"
                  min={0}
                  max={100}
                  defaultValue={vendor.spotlight.bright}
                  disabled={setVendor.isPending}
                  onMouseUp={(e) =>
                    setVendor.mutate({ id: cameraId, spotlight: { bright: Number((e.target as HTMLInputElement).value) } })
                  }
                  onTouchEnd={(e) =>
                    setVendor.mutate({ id: cameraId, spotlight: { bright: Number((e.target as HTMLInputElement).value) } })
                  }
                />
              </div>
              {vendor.ir && (
                <div className="mt-1.5 flex items-center justify-between">
                  <span className="text-xs text-white/75">Night vision (IR)</span>
                  <div className="flex overflow-hidden rounded-md border border-white/10">
                    {["Auto", "Off"].map((st) => (
                      <button
                        key={st}
                        disabled={setVendor.isPending}
                        onClick={() => setVendor.mutate({ id: cameraId, ir: st })}
                        className={`h-6 px-2.5 text-xs transition-colors [&+&]:border-l [&+&]:border-white/10 ${
                          vendor.ir!.state === st
                            ? "bg-brand/15 font-medium text-brand"
                            : "text-white/70 hover:bg-white/[0.06] hover:text-white"
                        }`}
                      >
                        {st}
                      </button>
                    ))}
                  </div>
                </div>
              )}
              {/* A refused change says why, in the camera's own terms, instead of a
                  control that silently snaps back. Cleared by the next attempt. */}
              {(setVendor.error || vendor.error) && (
                <p role="alert" className="mt-1.5 text-[11px] leading-snug text-danger">
                  {setVendor.error instanceof Error ? setVendor.error.message : vendor.error}
                </p>
              )}
            </div>
          )}

          {err && <p className="mt-2 text-[11px] leading-snug text-danger">{err}</p>}
        </div>
      )}
    </>
  );
}
