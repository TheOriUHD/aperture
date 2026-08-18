import { useEffect, useState } from "react";
import { SlidersHorizontal, X, Check, Loader2 } from "lucide-react";
import { useCameraOnvif, useSetCameraImaging, type OnvifImaging } from "../hooks/useCameras";

// On-video picture controls. The button only exists when this camera's ONVIF is
// switched on AND actually answering with adjustable settings — so it never
// appears as a dead control on a camera that can't do it.
export function CameraImagingOverlay({ cameraId, VidBtn }: {
  cameraId: string;
  VidBtn: React.ComponentType<{ onClick: () => void; label: string; active?: boolean; children: React.ReactNode }>;
}) {
  const { data } = useCameraOnvif(cameraId);
  const setImg = useSetCameraImaging();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<OnvifImaging | null>(null);
  const [err, setErr] = useState("");
  const [ok, setOk] = useState(false);

  useEffect(() => {
    if (data?.imaging) setDraft(data.imaging);
  }, [data]);

  // Nothing to offer → no button at all.
  if (!data?.enabled || !data.imaging || !draft) return null;

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
            {rows.map(({ key, label }) => {
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
                    onMouseUp={() => apply({ [key]: draft[key] } as Partial<OnvifImaging>)}
                    onTouchEnd={() => apply({ [key]: draft[key] } as Partial<OnvifImaging>)}
                  />
                </div>
              );
            })}

            {draft.irCutFilter && (
              <div>
                <span className="text-[11px] text-white/70">Day / night</span>
                <div className="mt-1 flex gap-1">
                  {(draft.irCutOptions?.length ? draft.irCutOptions : ["ON", "OFF", "AUTO"]).map((m) => (
                    <button
                      key={m}
                      disabled={setImg.isPending}
                      onClick={() => {
                        setDraft((d) => (d ? { ...d, irCutFilter: m } : d));
                        apply({ irCutFilter: m });
                      }}
                      className={`flex-1 rounded px-1.5 py-1 text-[11px] ${
                        draft.irCutFilter === m ? "bg-brand text-white" : "bg-white/10 text-white/70 hover:text-white"
                      }`}
                    >
                      {m === "ON" ? "Day" : m === "OFF" ? "Night" : "Auto"}
                    </button>
                  ))}
                </div>
              </div>
            )}
          </div>

          {err && <p className="mt-2 text-[11px] text-rec">{err}</p>}
        </div>
      )}
    </>
  );
}
