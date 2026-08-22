"use client";

import { useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n/context";
import { getPushVapidKey, subscribePush, unsubscribePush } from "@/lib/api";

// The VAPID public key arrives base64url-encoded (what the backend hands
// out and what a browser expects back); pushManager.subscribe wants raw
// bytes, so this is the one conversion in between.
function urlBase64ToUint8Array(base64: string): Uint8Array {
  const padding = "=".repeat((4 - (base64.length % 4)) % 4);
  const b64 = (base64 + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = window.atob(b64);
  return Uint8Array.from([...raw].map((c) => c.charCodeAt(0)));
}

type State = "unsupported" | "checking" | "off" | "on" | "denied" | "busy";

// A single icon-button living next to the theme/language toggles — turning
// notifications on is a per-device choice any signed-in person makes for
// themselves, not a server setting, so this asks nothing of the role that's
// logged in and needs no super_admin gate the way Settings does.
export function PushToggle() {
  const { t } = useI18n();
  const [state, setState] = useState<State>("checking");

  useEffect(() => {
    let cancelled = false;
    async function check() {
      if (!("serviceWorker" in navigator) || !("PushManager" in window) || !window.isSecureContext) {
        if (!cancelled) setState("unsupported");
        return;
      }
      if (Notification.permission === "denied") {
        if (!cancelled) setState("denied");
        return;
      }
      try {
        const reg = await navigator.serviceWorker.ready;
        const sub = await reg.pushManager.getSubscription();
        if (!cancelled) setState(sub ? "on" : "off");
      } catch {
        if (!cancelled) setState("off");
      }
    }
    check();
    return () => {
      cancelled = true;
    };
  }, []);

  if (state === "unsupported" || state === "checking") return null;

  const enable = async () => {
    setState("busy");
    try {
      const permission = await Notification.requestPermission();
      if (permission !== "granted") {
        setState(permission === "denied" ? "denied" : "off");
        return;
      }
      const { publicKey } = await getPushVapidKey();
      const reg = await navigator.serviceWorker.ready;
      const sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        // TS's DOM lib types Uint8Array as generic over its backing buffer,
        // which then fails to satisfy BufferSource even though this is
        // exactly the shape PushManager.subscribe expects at runtime.
        applicationServerKey: urlBase64ToUint8Array(publicKey) as BufferSource,
      });
      await subscribePush(sub.toJSON());
      setState("on");
    } catch {
      setState("off");
    }
  };

  const disable = async () => {
    setState("busy");
    try {
      const reg = await navigator.serviceWorker.ready;
      const sub = await reg.pushManager.getSubscription();
      if (sub) {
        await sub.unsubscribe();
        await unsubscribePush(sub.endpoint);
      }
      setState("off");
    } catch {
      setState("on");
    }
  };

  if (state === "denied") {
    return (
      <span className="min-h-9 rounded-md border border-[var(--color-border)] px-3 py-1.5 text-xs font-medium text-[var(--color-text-muted)]" title={t.push.denied}>
        🔕
      </span>
    );
  }

  return (
    <button
      onClick={state === "on" ? disable : enable}
      disabled={state === "busy"}
      aria-pressed={state === "on"}
      title={state === "on" ? t.push.enabled : t.push.enable}
      className={`min-h-9 rounded-md border px-3 py-1.5 text-xs font-medium disabled:opacity-60 ${
        state === "on"
          ? "border-[var(--color-primary)] text-[var(--color-primary)]"
          : "border-[var(--color-border)] text-[var(--color-text-muted)] hover:text-[var(--color-text)]"
      }`}
    >
      {state === "on" ? "🔔" : "🔕"}
    </button>
  );
}
