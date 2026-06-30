import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../lib/auth";
import { deviceNotifyEnabled } from "../lib/deviceNotify";
import { useUnreadAlarms } from "../lib/unreadAlarms";

interface StreamEvent {
  cameraId: string;
  camera: string;
  title: string;
  body: string;
  url: string;
  thumbUrl?: string;
}

// DeviceNotifications keeps a live SSE connection while the app is open. It
// always feeds the tab-title/favicon unread badge, and additionally raises a
// browser Notification when on-device notifications are enabled — no external
// push service (FCM/APNs) involved, so it works in Brave/hardened browsers.
export function DeviceNotifications() {
  const { user } = useAuth();
  const navigate = useNavigate();
  const { bump } = useUnreadAlarms();

  useEffect(() => {
    if (!user) return;
    const es = new EventSource("/api/notify/stream", { withCredentials: true });
    const lastShown = new Map<string, number>();

    es.onmessage = (e) => {
      let d: StreamEvent;
      try {
        d = JSON.parse(e.data);
      } catch {
        return;
      }

      // Tab badge: count it as unread unless the user is already on Alarms.
      if (window.location.pathname !== "/alarms") bump();

      // OS notification only when the user opted in on this device.
      if (!deviceNotifyEnabled()) return;
      const now = Date.now();
      if (now - (lastShown.get(d.cameraId) ?? 0) < 30_000) return; // per-camera cooldown
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
        /* notification can throw if permission was revoked */
      }
    };
    // EventSource reconnects automatically on transient errors.

    return () => es.close();
  }, [user, navigate, bump]);

  return null;
}
