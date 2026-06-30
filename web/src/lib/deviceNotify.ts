// On-device notifications that work without any external push service (FCM/APNs):
// while the app is open (a tab or the installed PWA), an SSE stream delivers
// motion events and the browser shows them via the Notification API. The
// preference is per-device (localStorage), not per-account.
const KEY = "aperture.deviceNotify";

export function deviceNotifySupported(): boolean {
  return typeof window !== "undefined" && "Notification" in window && "EventSource" in window;
}

export function deviceNotifyEnabled(): boolean {
  return (
    deviceNotifySupported() &&
    localStorage.getItem(KEY) === "1" &&
    Notification.permission === "granted"
  );
}

export async function enableDeviceNotify(): Promise<void> {
  if (!deviceNotifySupported()) {
    throw new Error("Notifications aren’t supported on this device/browser.");
  }
  const perm = await Notification.requestPermission();
  if (perm !== "granted") {
    throw new Error("Notification permission was denied. Allow notifications for this site and try again.");
  }
  localStorage.setItem(KEY, "1");
  window.dispatchEvent(new Event("aperture-devicenotify"));
}

export function disableDeviceNotify(): void {
  localStorage.setItem(KEY, "0");
  window.dispatchEvent(new Event("aperture-devicenotify"));
}
