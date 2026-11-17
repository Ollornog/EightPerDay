// SPDX-License-Identifier: MIT
// The service worker of EightPerDay: it keeps the assets of this build, so the program opens fast on a slow phone
// connection, and it never keeps a page, which carries personal data and changes. Without a connection a page says so.
const CACHE = "epd-__VERSION__";

self.addEventListener("install", (event) => {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener("activate", (event) => {
  event.waitUntil((async () => {
    for (const key of await caches.keys()) {
      if (key !== CACHE) await caches.delete(key);
    }
    await self.clients.claim();
  })());
});

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  if (event.request.method !== "GET" || url.origin !== self.location.origin) return;
  if (url.pathname.startsWith("/assets/")) {
    event.respondWith((async () => {
      const cache = await caches.open(CACHE);
      const hit = await cache.match(event.request);
      if (hit) return hit;
      const answer = await fetch(event.request);
      if (answer.ok) await cache.put(event.request, answer.clone());
      return answer;
    })());
    return;
  }
  if (event.request.mode === "navigate") {
    event.respondWith(fetch(event.request).catch(() => new Response(
      "<!doctype html><meta charset=utf-8><meta name=viewport content='width=device-width'><title>EightPerDay</title>" +
      "<p>No connection. / Keine Verbindung.</p>",
      { status: 503, headers: { "Content-Type": "text/html; charset=utf-8" } })));
  }
});
