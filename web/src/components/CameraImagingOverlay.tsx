import { useEffect, useState } from "react";
import { SlidersHorizontal, X, Check, Loader2, Lightbulb } from "lucide-react";
import {
  useCameraOnvif,
  useSetCameraImaging,
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
  const setImg = useSetCameraImaging();
  // Vendor extras (Reolink spotlight/IR) — fetched only once ONVIF is on.
  const { data: vendor } = useCameraVendor(cameraId, !!data?.enabled);
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
        <div className="absolute right-0 top-10 z-20 w-64 rounded-lg border border-white/10 bg-black/80 p-3 backdrop-blur-md shadow-pop">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-xs font-medium text-white/90">Picture</span>
            <div className="flex items-center gap-1.5">
              {setImg.isPending && <Loader2 className="h-3 w-3 animate-spin text-white/60" />}
              {ok && <Check className="h-3.5 w-3.5 text-online" />}
              <button onClick={() => setOpen(false)} className="text-white/50 hover:text-white" aria-label="Close">
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>

          <div className="space-y-2">
            {draft && rows.map(({ key, label }) => {
              const val = draft[key] as number | undefined;
              if (val == null) return null;
              const r = data.imaging?.ranges?.[key] ?? { min: 0, max: 255 };
              return (
                <div key={key}>
                  <div className="flex items-center justify-between">
                    <span className="text-[11px] text-white/70">{label}</span>
                    <span className="text-[11px] tabular-nums text-white/50">{Math.round(val)}</span>
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
                <span className="text-[11px] text-white/70">Day / night</span>
                <div className="mt-1 flex gap-1">
                  {(draft?.irCutOptions?.length ? draft.irCutOptions : ["ON", "OFF", "AUTO"]).map((m) => (
                    <button
                      key={m}
                      disabled={setImg.isPending}
                      onClick={() => {
                        setDraft((d) => (d ? { ...d, irCutFilter: m } : d));
                        apply({ irCutFilter: m });
                      }}
                      className={`flex-1 rounded px-1.5 py-1 text-[11px] ${
                        draft?.irCutFilter === m ? "bg-brand text-white" : "bg-white/10 text-white/70 hover:text-white"
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
            <div className="mt-3 border-t border-white/10 pt-2.5">
              <div className="flex items-center justify-between">
                <span className="flex items-center gap-1.5 text-[11px] text-white/70">
                  <Lightbulb className="h-3.5 w-3.5" /> Spotlight
                </span>
                <button
                  disabled={setVendor.isPending}
                  onClick={() =>
                    setVendor.mutate({
                      id: cameraId,
                      spotlight: { state: vendor.spotlight!.state === 1 ? 0 : 1 },
                    })
                  }
                  className={`rounded px-2.5 py-1 text-[11px] font-medium ${
                    vendor.spotlight.state === 1
                      ? "bg-[#e6b45a] text-black"
                      : "bg-white/10 text-white/70 hover:text-white"
                  }`}
                >
                  {vendor.spotlight.state === 1 ? "On" : "Off"}
                </button>
              </div>
              <div className="mt-1.5">
                <div className="flex items-center justify-between">
                  <span className="text-[11px] text-white/50">Brightness</span>
                  <span className="text-[11px] tabular-nums text-white/50">{vendor.spotlight.bright}</span>
                </div>
                <input
                  type="range"
                  className="w-full accent-[#e6b45a]"
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
                  <span className="text-[11px] text-white/70">Night vision (IR)</span>
                  <div className="flex gap-1">
                    {["Auto", "Off"].map((st) => (
                      <button
                        key={st}
                        disabled={setVendor.isPending}
                        onClick={() => setVendor.mutate({ id: cameraId, ir: st })}
                        className={`rounded px-2 py-0.5 text-[11px] ${
                          vendor.ir!.state === st ? "bg-brand text-white" : "bg-white/10 text-white/70 hover:text-white"
                        }`}
                      >
                        {st}
                      </button>
                    ))}
                  </div>
                </div>
              )}
              {vendor.error && <p className="mt-1 text-[11px] text-rec">{vendor.error}</p>}
            </div>
          )}

          {err && <p className="mt-2 text-[11px] text-rec">{err}</p>}
        </div>
      )}
    </>
  );
}
