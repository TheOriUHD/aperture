import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

// ChangeFeedListener subscribes to the v1 change feed (SSE, T2.5) and refreshes
// the affected react-query caches on each message. Verified in a browser to drive
// updates live, so the polls it replaces have now been removed ("pin before
// remove"): system-status/cameras (camera.state + alarm.state), storage and
// storage-locations (storage.updated), exports (export.progress), and every
// events list incl. the dashboard cards (event.*/detection.*). Three of those keep
// a slow 60s backstop ALONGSIDE the feed (feed still primary): system-status, whose
// uptime/go2rtc counters free-run between transitions, and storage plus
// storage-locations, whose disk fills from outside Aperture with no mutation to
// emit storage.updated (per-location used/free bytes drift the same way). Polls with
// NO corresponding feed message stay as full polls by design — bitrate, Home
// Assistant config/sensors, the live snapshot thumbnails, and the per-day
// segments/coverage rail (segments carry no per-row feed message). Additive and
// safe: an unknown or mis-keyed message type is a no-op. Mounted only when
// authenticated (rendered next to DeviceNotifications in App).
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
        inval("events", "events-today", "events-all", "events-recent-dash");
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
