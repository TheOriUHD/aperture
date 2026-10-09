import { useEffect, useState } from "react";
import { Loader2, SlidersHorizontal, RotateCcw } from "lucide-react";
import { Switch } from "./ui/Switch";
import {
  useCameraOnvif,
  useSetCameraOnvif,
  useSetCameraImaging,
  type OnvifImaging,
} from "../hooks/useCameras";

// Picture settings read from (and written back to) the camera over ONVIF —
// brightness, contrast, saturation, sharpness and day/night — so they can be
// adjusted while watching the feed instead of in the vendor's web UI.
//
// Everything here is optional and additive: it only contacts the camera once
// switched on, only renders controls the camera actually reports, and a camera
// that refuses or is unreachable shows the reason rather than breaking the page.
// vendorMarked: the camera is marked as a vendor (Reolink) in the editor. Its local
// controls sign in with the same camera account as ONVIF, so the account fields
// must be reachable even while picture settings are switched off.
export function CameraOnvifPanel({ cameraId, vendorMarked = false }: { cameraId: string; vendorMarked?: boolean }) {
  const { data, isFetching } = useCameraOnvif(cameraId);
  const setCfg = useSetCameraOnvif();
  const setImg = useSetCameraImaging();

  const [user, setUser] = useState("");
  const [pass, setPass] = useState("");
  const [port, setPort] = useState(0);
  const [draft, setDraft] = useState<OnvifImaging | null>(null);
  const [err, setErr] = useState("");
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    if (!data) return;
    setPort(data.port);
    setDraft(data.imaging ?? null);
  }, [data]);

  if (!data) return null;
  const probe = data.probe;
  const busy = setCfg.isPending || setImg.isPending || isFetching;
  const hasAccount = data.hasUser || data.hasPassword;

  async function save(patch: Parameters<typeof setCfg.mutateAsync>[0]): Promise<boolean> {
    setErr("");
    try {
      await setCfg.mutateAsync(patch);
      return true;
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Could not save.");
      return false;
    }
  }

  // The account is write-only: the saved values never reach the browser, so the
  // fields start empty and anything typed replaces what is saved.
  function saveField(patch: { user: string } | { password: string }, clear: () => void) {
    save({ id: cameraId, ...patch }).then((ok) => ok && clear());
  }

  async function apply(next: Partial<OnvifImaging>) {
    setErr("");
    setSaved(false);
    try {
      await setImg.mutateAsync({ id: cameraId, ...next });
      setSaved(true);
      setTimeout(() => setSaved(false), 1500);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "The camera rejected the change.");
    }
  }

  const sliders: { key: keyof OnvifImaging; label: string }[] = [
    { key: "brightness", label: "Brightness" },
    { key: "contrast", label: "Contrast" },
    { key: "saturation", label: "Saturation" },
    { key: "sharpness", label: "Sharpness" },
  ];

  return (
    <div className="rounded-lg border border-line bg-ink-850 p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <SlidersHorizontal className="h-4 w-4 text-fg-muted" />
            <span className="text-sm font-medium text-fg">Camera picture settings</span>
            {probe?.ok && probe.device.model && (
              <span className="pill bg-ink-700 text-fg-faint">{probe.device.model}</span>
            )}
          </div>
          <p className="mt-0.5 text-[11px] text-fg-faint leading-relaxed">
            Adjust the camera&apos;s own image settings over ONVIF, live. Off by default; nothing
            contacts the camera over ONVIF until you switch it on.
          </p>
        </div>
        <Switch
          checked={data.enabled}
          disabled={busy}
          label="Camera picture settings"
          onChange={(v) => save({ id: cameraId, enabled: v })}
        />
      </div>

      {/* The account Aperture signs in to the camera itself with. ONVIF picture
          settings AND the vendor controls (Reolink spotlight / IR) both use it, so it
          is labelled for what it is and shown whenever either is in play — not only
          while picture settings are on, which is where it used to hide. */}
      {(data.enabled || vendorMarked) && (
        <div className="mt-3">
          <p className="text-xs font-medium text-fg-muted">Camera account</p>
          <div className={`mt-1.5 grid gap-2 ${data.enabled ? "grid-cols-3" : "grid-cols-2"}`}>
            <div>
              <label className="label">Username</label>
              <input
                className="input h-8 text-xs"
                placeholder={data.hasUser ? "••••• (saved)" : "from stream link"}
                value={user}
                onChange={(e) => setUser(e.target.value)}
                onBlur={() => user.trim() && saveField({ user: user.trim() }, () => setUser(""))}
                autoComplete="off"
                spellCheck={false}
              />
            </div>
            <div>
              <label className="label">Password</label>
              <input
                className="input h-8 text-xs"
                type="password"
                placeholder={data.hasPassword ? "••••• (saved)" : "from stream link"}
                value={pass}
                onChange={(e) => setPass(e.target.value)}
                onBlur={() => pass && saveField({ password: pass }, () => setPass(""))}
                // Never let the browser autofill the user's own Aperture password here.
                autoComplete="new-password"
              />
            </div>
            {data.enabled && (
              <div>
                <label className="label">ONVIF port</label>
                <input
                  className="input h-8 text-xs"
                  type="number"
                  placeholder="auto"
                  value={port || ""}
                  onChange={(e) => setPort(Number(e.target.value) || 0)}
                  onBlur={() => port !== data.port && save({ id: cameraId, port })}
                />
              </div>
            )}
          </div>
          <p className="mt-1.5 text-[11px] leading-relaxed text-fg-faint">
            How Aperture signs in to the camera itself, for{" "}
            {data.enabled && vendorMarked
              ? "picture settings and the Reolink spotlight and night vision"
              : vendorMarked
                ? "the Reolink spotlight and night vision"
                : "picture settings"}
            .{" "}
            {hasAccount
              ? "The saved account stays hidden; type in a field to replace it."
              : "Leave blank to use the login from the stream link."}
            {hasAccount && (
              <>
                {" "}
                <button
                  type="button"
                  className="text-fg-muted underline underline-offset-2 hover:text-fg disabled:opacity-50"
                  disabled={busy}
                  onClick={() => save({ id: cameraId, user: "", password: "" })}
                >
                  Use the stream link&apos;s login instead
                </button>
              </>
            )}
          </p>
          {err && !data.enabled && <p className="mt-1.5 text-xs text-rec">{err}</p>}
        </div>
      )}

      {data.enabled && (
        <div className="mt-3 space-y-3">
          {isFetching && !probe && (
            <p className="flex items-center gap-2 text-xs text-fg-faint">
              <Loader2 className="h-3.5 w-3.5 animate-spin" /> Contacting the camera…
            </p>
          )}

          {probe && !probe.ok && (
            <p className="text-xs text-rec">{probe.error || "The camera did not answer on any ONVIF port."}</p>
          )}
          {probe?.ok && !probe.hasImaging && (
            <p className="text-xs text-fg-faint">This camera answers ONVIF but exposes no picture settings.</p>
          )}
          {data.imagingError && <p className="text-xs text-rec">{data.imagingError}</p>}

          {draft && (
            <div className="space-y-2.5">
              {sliders.map(({ key, label }) => {
                const val = draft[key] as number | undefined;
                if (val == null) return null; // the camera doesn't expose this one
                const r = data.imaging?.ranges?.[key] ?? { min: 0, max: 255 };
                return (
                  <div key={key}>
                    <div className="flex items-center justify-between">
                      <label className="label mb-0">{label}</label>
                      <span className="text-xs tabular-nums text-fg-muted">{Math.round(val)}</span>
                    </div>
                    <input
                      type="range"
                      className="w-full accent-brand"
                      min={r.min}
                      max={r.max}
                      value={val}
                      disabled={busy}
                      onChange={(e) =>
                        setDraft((d) => (d ? { ...d, [key]: Number(e.target.value) } : d))
                      }
                      // Apply on release, so dragging doesn't spam the camera.
                      onMouseUp={() => apply({ [key]: draft[key] } as Partial<OnvifImaging>)}
                      onTouchEnd={() => apply({ [key]: draft[key] } as Partial<OnvifImaging>)}
                    />
                  </div>
                );
              })}

              {draft.irCutFilter && (
                <div>
                  <label className="label">Day / night (IR cut filter)</label>
                  <div className="flex gap-1.5">
                    {(draft.irCutOptions?.length ? draft.irCutOptions : ["ON", "OFF", "AUTO"]).map((m) => (
                      <button
                        key={m}
                        disabled={busy}
                        onClick={() => {
                          setDraft((d) => (d ? { ...d, irCutFilter: m } : d));
                          apply({ irCutFilter: m });
                        }}
                        className={`rounded-md px-2.5 py-1 text-xs ${
                          draft.irCutFilter === m ? "bg-brand text-white" : "bg-ink-700 text-fg-muted hover:text-fg"
                        }`}
                      >
                        {m === "ON" ? "Day" : m === "OFF" ? "Night" : "Auto"}
                      </button>
                    ))}
                  </div>
                </div>
              )}

              <div className="flex items-center gap-2">
                {saved && <span className="text-xs text-online">Applied to the camera.</span>}
                {data.imaging && (
                  <button
                    className="ml-auto inline-flex items-center gap-1.5 text-[11px] text-fg-muted hover:text-fg"
                    disabled={busy}
                    onClick={() => setDraft(data.imaging ?? null)}
                  >
                    <RotateCcw className="h-3 w-3" /> Revert unsaved
                  </button>
                )}
              </div>
            </div>
          )}

          {err && <p className="text-xs text-rec">{err}</p>}
        </div>
      )}
    </div>
  );
}
