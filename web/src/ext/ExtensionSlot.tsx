import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "../lib/api";
import { OSD_ALPHA_VAR } from "../lib/osd";
import { panelsForSlot, type ExtPanel } from "./panels";
import { useExtPanels } from "./useExtPanels";

// ExtensionSlot mounts the sandboxed iframes of every enabled extension that
// contributes to `slot`. It renders NOTHING when there are none — the property
// that keeps every host surface identical with zero extensions installed.
export function ExtensionSlot({ slot, cameraId }: { slot: string; cameraId?: string }) {
  const { data: panels = [] } = useExtPanels();
  // For camera-detail, the panel's camera grant also gates its PRESENCE: a panel
  // granted only some cameras renders on exactly those and nowhere else.
  const forSlot = panelsForSlot(panels, slot, cameraId);
  if (forSlot.length === 0) return null;
  return (
    <>
      {forSlot.map((p) => (
        <ExtensionPanel key={p.extId + ":" + p.entry} panel={p} cameraId={cameraId} />
      ))}
    </>
  );
}

// ExtensionPanel is one sandboxed iframe plus the host side of the SDK bridge: it
// relays the panel's postMessage API calls to the broker (which enforces
// caller∩extension authority) and posts replies back. The iframe uses
// sandbox="allow-scripts" WITHOUT allow-same-origin, so it is an opaque origin
// with no cookies and no access to this window — it can reach Aperture only
// through this bridge.
// Chrome decides what the host wraps a panel in:
//   "card" — the default flow layout: a bordered card in a column of cards.
//   "bare" — no wrapper and a transparent background, for a panel that is itself
//     the widget (a player OSD, or the body of a toolbar dropdown). The host still
//     owns position and size; the panel only asks for a height.
export type ExtChrome = "card" | "bare";

export function ExtensionPanel({
  panel,
  cameraId,
  chrome = "card",
  width,
  initialHeight = 180,
}: {
  panel: ExtPanel;
  cameraId?: string;
  chrome?: ExtChrome;
  width?: number | string;
  initialHeight?: number;
}) {
  const ref = useRef<HTMLIFrameElement>(null);
  // An overlay panel starts at zero height and is revealed only once it asks for a
  // size via resize(), so a conditional OSD that decides it has nothing to show on
  // this camera occupies nothing at all. Card panels instead keep their reserved
  // height, because there an empty frame sits inside a card and reads as "loading"
  // rather than as damage.
  const overlay = chrome === "bare";
  const [height, setHeight] = useState(overlay ? 0 : initialHeight);
  // An overlay also negotiates its WIDTH. Without this a widget that collapses to a
  // small button still occupies the host's full reserved width, and that invisible
  // column sits over the video swallowing clicks meant for the picture.
  const [negotiatedWidth, setNegotiatedWidth] = useState<number>();

  useEffect(() => {
    const iframe = ref.current;
    const onMessage = async (e: MessageEvent) => {
      if (!iframe || e.source !== iframe.contentWindow) return; // only this panel's iframe
      const m = e.data as { __apReq?: boolean; id?: number; type?: string; payload?: Record<string, unknown> };
      if (!m || !m.__apReq) return;
      if (m.type === "ui.resize") {
        // NOT `Number(x) || 180`: zero is falsy, so that coerced a panel's explicit
        // "I have nothing to show" into a 180px frame — defeating the zero-height
        // path below and leaving an empty box sitting on the video. Only a value
        // that is not a number at all falls back.
        const asked = Number(m.payload?.height);
        const h = Number.isFinite(asked) ? asked : 180;
        // Width is optional. The SDK's automatic sizing reports height only, so an
        // absent width must LEAVE the current one alone rather than reset it.
        const askedW = Number(m.payload?.width);
        if (overlay && Number.isFinite(askedW) && askedW > 0) {
          setNegotiatedWidth(Math.min(askedW, 4000));
        }
        // An overlay may legitimately ask for nothing — a conditional widget that has
        // decided it has no controls for this camera. Honour a zero rather than
        // clamping it up to 80, or it leaves a blank strip sitting on the video.
        setHeight(overlay ? Math.min(Math.max(0, h), 4000) : Math.max(80, Math.min(h, 4000)));
        return;
      }
      const reply = (result: unknown) =>
        iframe.contentWindow?.postMessage({ __ap: true, id: m.id, result }, "*");
      const fail = (err: unknown) =>
        iframe.contentWindow?.postMessage({ __ap: true, id: m.id, error: (err as Error).message }, "*");

      // All host-relayed calls carry the panel's TRUSTED extId — never one the
      // sandboxed panel supplies — so a panel can only ever reach its own broker
      // authority and its own private storage namespace.
      const p = m.payload || {};
      const kv = `/api/ext/${panel.extId}/kv`;
      const key = () => encodeURIComponent(String(p.key ?? ""));
      try {
        switch (m.type) {
          case "api.request":
            reply(await api.post(`/api/ext/${panel.extId}/broker`, { method: p.method, path: p.path, body: p.body }));
            break;
          case "storage.get":
            try {
              const r = await api.get<{ value: string }>(`${kv}/${key()}`);
              reply(r.value);
            } catch (err) {
              if (err instanceof ApiError && err.status === 404) reply(null); // absent key
              else throw err;
            }
            break;
          case "storage.set":
            await api.put(`${kv}/${key()}`, { value: String(p.value ?? "") });
            reply(null);
            break;
          case "storage.delete":
            await api.del(`${kv}/${key()}`);
            reply(null);
            break;
          case "storage.keys":
            reply((await api.get<{ keys: string[] }>(kv)).keys);
            break;
          case "settings.get": {
            const sv = await api.get<{ values: Record<string, string> }>(`/api/ext/${panel.extId}/settings`);
            reply(sv.values[String(p.key ?? "")] ?? null); // non-secret only; secrets are never returned
            break;
          }
          case "settings.all":
            reply((await api.get<{ values: Record<string, string> }>(`/api/ext/${panel.extId}/settings`)).values);
            break;
          case "net.fetch":
            // Host-mediated egress to a declared host; {{secret:key}} placeholders are
            // expanded server-side, so a secret never travels through this bridge.
            reply(
              await api.post(`/api/ext/${panel.extId}/fetch`, {
                url: p.url,
                method: p.method,
                headers: p.headers,
                body: p.body,
              }),
            );
            break;
        }
      } catch (err) {
        fail(err);
      }
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [panel.extId]);

  // The panel is a separate document, so it cannot inherit :root variables or read
  // React context. The host's overlay opacity is therefore handed over in the URL,
  // alongside the camera — the same one-way host→panel channel that already exists.
  // A panel that ignores it simply looks like every other panel that ignores it;
  // one that honours it matches first-party chrome exactly.
  const params = new URLSearchParams();
  if (cameraId) params.set("camera", cameraId);
  if (chrome === "bare") {
    params.set(
      "osdAlpha",
      typeof document !== "undefined"
        ? getComputedStyle(document.documentElement).getPropertyValue(OSD_ALPHA_VAR).trim() || "0.55"
        : "0.55",
    );
  }
  const qs = params.toString();
  const src = `/ext/${panel.extId}/${panel.entry}` + (qs ? `?${qs}` : "");

  const frame = (
    <iframe
      ref={ref}
      title={panel.title || panel.extName}
      sandbox="allow-scripts"
      src={src}
      // allowTransparency is non-standard but still required by some engines for an
      // iframe to composite over what is behind it; the modern path is the panel
      // setting `background: transparent` on its own body, which the docs mandate.
      style={{
        width: negotiatedWidth ?? width ?? "100%",
        height,
        border: 0,
        // An unsized or zero-height overlay is hidden by its HEIGHT, never by
        // display:none. display:none suppresses layout inside the frame, so
        // documentElement.scrollHeight there is always 0 — a panel hidden that way
        // can never measure itself back into view, and the SDK's ResizeObserver
        // would keep reporting 0 forever. At height 0 the frame still lays out (and
        // so can measure) while painting nothing.
        display: "block",
        // The overlay slots composite over live video, so the frame itself must not
        // paint — and background:transparent is only half of that. color-scheme
        // INHERITS, so this element otherwise computes `dark` from the app's :root
        // while a panel document that declares nothing computes light; on that
        // mismatch the UA is REQUIRED to give the frame an opaque canvas (CSS Color
        // Adjust §2.4), which is what painted a white rectangle over the picture.
        // Pin this side explicitly and inject the matching :root rule into every
        // panel (extPanelBase, internal/api/ext_static.go) so the two always agree.
        colorScheme: "dark",
        background: "transparent",
      }}
    />
  );
  if (chrome === "bare") return frame;
  return <div className="card overflow-hidden">{frame}</div>;
}
