// Aperture service worker. Deliberately does NOT cache app assets (the server
// already controls caching: no-store HTML + immutable hashed assets), so it
// can never serve a stale bundle. Its job is Web Push + notification routing.

self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()));

self.addEventListener("push", (event) => {
  let data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch (e) {
    data = { title: "Aperture", body: event.data ? event.data.text() : "" };
  }
  const title = data.title || "Aperture";
  const options = {
    body: data.body || "",
    icon: "/icon-192.png",
    badge: "/icon-192.png",
    image: data.image || undefined,
    data: { url: data.url || "/" },
    tag: data.url || undefined,
    renotify: true,
  };
  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const url = (event.notification.data && event.notification.data.url) || "/";
  event.waitUntil(
    (async () => {
      const wins = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
      for (const c of wins) {
        if ("focus" in c) {
          try {
            await c.navigate(url);
          } catch (e) {
            /* cross-origin navigate may fail; fall through */
          }
          return c.focus();
        }
      }
      return self.clients.openWindow(url);
    })(),
  );
});
