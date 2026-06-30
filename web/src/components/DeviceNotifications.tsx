import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../lib/auth";
import { deviceNotifyEnabled } from "../lib/deviceNotify";

interface StreamEvent {
  cameraId: string;
  camera: string;
  title: string;
  body: string;
  url: string;
  thumbUrl?: string;
}

// DeviceNotifications keeps a live SSE connection (when on-device notifications
// are enabled) and surfaces motion events via the browser Notification API. No
// external push service is involved, so it works in Brave/hardened browsers.
export function DeviceNotifications() {
  const { user } = useAuth();
  const navigate = useNavigate();

  useEffect(() => {
    if (!user) return;
    let es: EventSource | null = null;
    const lastShown = new Map<string, number>();

    const close = () => {
      es?.close();
      es = null;
    };

    const connect = () => {
      if (!deviceNotifyEnabled()) {
        close();
        return;
      }
      if (es) return;
      es = new EventSource("/api/notify/stream", { withCredentials: true });
      es.onmessage = (e) => {
        let d: StreamEvent;
        try {
          d = JSON.parse(e.data);
        } catch {
          return;
        }
        // Per-camera cooldown so a flurry of motion doesn't spam.
        const now = Date.now();
        if (now - (lastShown.get(d.cameraId) ?? 0) < 30_000) return;
        lastShown.set(d.cameraId, now);

        try {
          const n = new Notification(d.title || `Motion · ${d.camera}`, {
            body: d.body,
            icon: d.thumbUrl || "/favicon-32.png",
            tag: d.cameraId,
          });
          n.onclick = () => {
            window.focus();
            try {
              const u = new URL(d.url, window.location.origin);
              navigate(u.pathname + u.search);
            } catch {
              /* ignore */
            }
            n.close();
          };
        } catch {
          /* notification construction can throw if permission was revoked */
        }
      };
      // EventSource reconnects automatically on error; nothing to do.
    };

    connect();
    window.addEventListener("aperture-devicenotify", connect);
    return () => {
      window.removeEventListener("aperture-devicenotify", connect);
      close();
    };
  }, [user, navigate]);

  return null;
}
