// Aruzor service worker.
//
// Deliberately narrow. A monitoring dashboard is the one kind of app where a
// cached answer is actively harmful: showing yesterday's CPU reading as if it
// were current is worse than showing nothing at all. So this caches the app
// shell — the HTML, JS, CSS and icons needed to start — and never touches
// /api. Offline, the app loads and reports that it cannot reach the server,
// which is the honest answer.

const VERSION = "aruzor-v1";
const SHELL_CACHE = `${VERSION}-shell`;

// Only assets that are safe to serve stale: they are content-hashed, and a
// deployment id is appended to every static request, so a new build asks for
// different URLs rather than reusing these.
const PRECACHE = ["/manifest.json", "/icon-192.png", "/icon-512.png", "/logo-mark.png"];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(SHELL_CACHE)
      // Individually, so one missing file cannot fail the whole install and
      // leave the app with no worker at all.
      .then((cache) => Promise.allSettled(PRECACHE.map((url) => cache.add(url))))
      .then(() => self.skipWaiting()),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => !k.startsWith(VERSION)).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("fetch", (event) => {
  const { request } = event;
  if (request.method !== "GET") return;

  const url = new URL(request.url);

  // Never our own origin's API, and never another origin.
  if (url.origin !== self.location.origin) return;
  if (url.pathname.startsWith("/api/")) return;

  // Navigations go to the network first so a deploy is picked up immediately;
  // the cached shell is only a fallback for being offline.
  if (request.mode === "navigate") {
    event.respondWith(
      fetch(request)
        .then((response) => {
          const copy = response.clone();
          caches.open(SHELL_CACHE).then((cache) => cache.put("/", copy));
          return response;
        })
        .catch(() => caches.match("/").then((cached) => cached ?? Response.error())),
    );
    return;
  }

  // Static assets are immutable per build, so cache-first is safe and makes
  // a repeat launch instant.
  if (url.pathname.startsWith("/_next/static/") || PRECACHE.includes(url.pathname)) {
    event.respondWith(
      caches.match(request).then((cached) => {
        if (cached) return cached;
        return fetch(request).then((response) => {
          if (response.ok) {
            const copy = response.clone();
            caches.open(SHELL_CACHE).then((cache) => cache.put(request, copy));
          }
          return response;
        });
      }),
    );
  }
});

// The backend sends {title, body} as the whole payload — no rich content,
// no actions to render unreviewed. A push with no data at all (some push
// services deliver an empty wakeup ping) falls back to a generic line
// rather than throwing and dropping the notification.
self.addEventListener("push", (event) => {
  let payload = { title: "Aruzor", body: "Yeni bir bildirim var." };
  if (event.data) {
    try {
      payload = { ...payload, ...event.data.json() };
    } catch {
      payload.body = event.data.text();
    }
  }

  event.waitUntil(
    self.registration.showNotification(payload.title, {
      body: payload.body,
      icon: "/icon-192.png",
      badge: "/icon-192.png",
      // Alerts about the same monitor collapse into the latest one instead
      // of stacking — a phone that missed three checks needs one thing to
      // tap, not three.
      tag: "aruzor-alert",
      renotify: true,
    }),
  );
});

// Clicking the notification should land on the app, reusing an already-open
// tab rather than piling up a new one every time.
self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  event.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((clients) => {
      for (const client of clients) {
        if ("focus" in client) return client.focus();
      }
      if (self.clients.openWindow) return self.clients.openWindow("/");
    }),
  );
});
