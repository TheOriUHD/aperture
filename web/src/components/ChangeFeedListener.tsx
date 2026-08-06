import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

// ChangeFeedListener subscribes to the v1 change feed (SSE, T2.5) and refreshes
// the affected react-query caches on each message. It runs ALONGSIDE the existing
// polls — those stay as the guard until this is verified in a browser to actually
// drive updates, and only then are they dropped ("pin before remove"). Additive
// and safe: an unknown or mis-keyed message type is a no-op, so a silent or
// partial feed simply falls back to the polls. Mounted only when authenticated
// (rendered next to DeviceNotifications in App).
export function ChangeFeedListener() {
  const qc = useQueryClient();
  useEffect(() => {
    const es = new EventSource("/api/v1/feed", { withCredentials: true });
    const inval = (...keys: string[]) =>
      keys.forEach((k) => qc.invalidateQueries({ queryKey: [k] }));
    es.onmessage = (m) => {
      let type = "";
      try {
        type = (JSON.parse(m.data) as { type?: string }).type ?? "";
      } catch {
        return;
      }
      if (type.startsWith("event.") || type.startsWith("detection.")) {
        inval("events", "events-today", "events-all");
      } else if (type === "camera.state") {
        inval("cameras", "system-status");
      } else if (type === "storage.updated") {
        inval("storage", "storage-locations");
      } else if (type === "config.updated") {
        inval("settings", "site", "site-name", "schedule");
      } else if (type === "automation.updated") {
        inval("rules", "automations", "channels", "webhooks");
      } else if (type === "alarm.state") {
        inval("system-status");
      } else if (type === "export.progress") {
        inval("exports");
      }
    };
    return () => es.close();
  }, [qc]);
  return null;
}
