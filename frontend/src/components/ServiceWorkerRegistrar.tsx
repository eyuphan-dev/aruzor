"use client";

import { useEffect } from "react";

// Registers the service worker, which is what makes the app installable to a
// phone's home screen. Kept as its own component so the root layout stays a
// server component.
export function ServiceWorkerRegistrar() {
  useEffect(() => {
    if (!("serviceWorker" in navigator)) return;
    // Service workers need a secure context. Over plain HTTP (a bare-IP
    // install, say) registration throws, so it is skipped rather than
    // logging an error the operator can do nothing about.
    if (!window.isSecureContext) return;

    navigator.serviceWorker.register("/sw.js").catch(() => {
      // A failed registration costs the install prompt and nothing else —
      // the app itself works exactly the same without it.
    });
  }, []);

  return null;
}
