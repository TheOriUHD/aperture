import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "../lib/api";
import { panelsForSlot, type ExtPanel } from "./panels";
import { useExtPanels } from "./useExtPanels";

// ExtensionSlot mounts the sandboxed iframes of every enabled extension that
// contributes to `slot`. It renders NOTHING when there are none — the property
// that keeps every host surface identical with zero extensions installed.
export function ExtensionSlot({ slot, cameraId }: { slot: string; cameraId?: string }) {
  const { data: panels = [] } = useExtPanels();
  const forSlot = panelsForSlot(panels, slot);
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
export function ExtensionPanel({ panel, cameraId }: { panel: ExtPanel; cameraId?: string }) {
  const ref = useRef<HTMLIFrameElement>(null);
  const [height, setHeight] = useState(180);

  useEffect(() => {
    const iframe = ref.current;
    const onMessage = async (e: MessageEvent) => {
      if (!iframe || e.source !== iframe.contentWindow) return; // only this panel's iframe
      const m = e.data as { __apReq?: boolean; id?: number; type?: string; payload?: Record<string, unknown> };
      if (!m || !m.__apReq) return;
      if (m.type === "ui.resize") {
        const h = Number(m.payload?.height) || 180;
        setHeight(Math.max(80, Math.min(h, 4000)));
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
        }
      } catch (err) {
        fail(err);
      }
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [panel.extId]);

  const src = `/ext/${panel.extId}/${panel.entry}` + (cameraId ? `?camera=${encodeURIComponent(cameraId)}` : "");
  return (
    <div className="card overflow-hidden">
      <iframe
        ref={ref}
        title={panel.title || panel.extName}
        sandbox="allow-scripts"
        src={src}
        style={{ width: "100%", height, border: 0, display: "block" }}
      />
    </div>
  );
}
