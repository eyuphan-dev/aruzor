"use client";

import { useEffect, useState } from "react";
import Image from "next/image";
import { usePathname } from "next/navigation";
import { useI18n } from "@/lib/i18n/context";

// iOS never fires beforeinstallprompt and has no install API at all — the
// only way onto the home screen is Share → Add to Home Screen, a path nobody
// finds on their own. Without a hint here, "the PWA doesn't work on iPhone"
// is just what having no install affordance looks like from the outside.
const DISMISSED_KEY = "aruzor-install-dismissed";

type Platform = "android" | "ios" | null;

function detectPlatform(): Platform {
  const ua = window.navigator.userAgent;
  const standalone =
    window.matchMedia("(display-mode: standalone)").matches ||
    // iOS Safari's own flag, not covered by the media query on older versions.
    (window.navigator as Navigator & { standalone?: boolean }).standalone === true;
  if (standalone) return null;

  const isIos = /iphone|ipad|ipod/i.test(ua) || (ua.includes("Macintosh") && "ontouchend" in document);
  if (isIos) return "ios";

  // Chrome-family Android — the only browsers that implement beforeinstallprompt.
  if (/android/i.test(ua)) return "android";

  return null;
}

export function InstallPrompt() {
  const { t } = useI18n();
  const pathname = usePathname();
  const [platform, setPlatform] = useState<Platform>(null);
  const [deferredPrompt, setDeferredPrompt] = useState<Event | null>(null);
  const [visible, setVisible] = useState(false);

  // The login screen is short enough that a fixed bottom banner sits on top
  // of the submit button — and someone who hasn't signed in yet is not the
  // moment to ask them to install anything.
  const suppressed = pathname === "/login";

  useEffect(() => {
    if (suppressed) return;
    if (window.localStorage.getItem(DISMISSED_KEY)) return;

    const detected = detectPlatform();
    if (detected === "ios") {
      // Reads browser/DOM state (UA, display-mode) once at mount to decide
      // whether to show the hint at all — synchronizing with the platform,
      // not reacting to React state, so this is the effect's job.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setPlatform("ios");
      setVisible(true);
      return;
    }

    // Android only becomes visible once Chrome actually offers to install —
    // otherwise this would show up on desktop Chrome and other browsers that
    // never fire the event, with a button that does nothing.
    const onBeforeInstall = (event: Event) => {
      event.preventDefault();
      setDeferredPrompt(event);
      setPlatform("android");
      setVisible(true);
    };
    window.addEventListener("beforeinstallprompt", onBeforeInstall);
    return () => window.removeEventListener("beforeinstallprompt", onBeforeInstall);
  }, [suppressed]);

  if (!visible || !platform || suppressed) return null;

  const dismiss = () => {
    window.localStorage.setItem(DISMISSED_KEY, "1");
    setVisible(false);
  };

  const install = async () => {
    if (!deferredPrompt) return;
    // Chrome's own prompt type; not in lib.dom.d.ts, so accessed loosely here.
    await (deferredPrompt as unknown as { prompt: () => Promise<void> }).prompt();
    dismiss();
  };

  const copy = platform === "ios" ? t.install.ios : t.install.android;

  return (
    <div
      role="status"
      className="fixed inset-x-3 bottom-[calc(4.75rem+env(safe-area-inset-bottom))] z-40 mx-auto flex max-w-sm items-start gap-3 rounded-xl border p-3 shadow-lg md:bottom-4 md:left-4 md:right-auto md:mx-0"
      style={{ background: "var(--color-card)", borderColor: "var(--color-border)" }}
    >
      <Image src="/icon-192.png" alt="" width={36} height={36} className="mt-0.5 shrink-0 rounded-lg" />
      <div className="min-w-0 flex-1">
        <p className="text-sm font-semibold" style={{ color: "var(--color-text)" }}>
          {copy.title}
        </p>
        <p className="mt-0.5 text-xs leading-relaxed" style={{ color: "var(--color-text-muted)" }}>
          {copy.body}
        </p>
        <div className="mt-2 flex items-center gap-3">
          {platform === "android" && (
            <button
              type="button"
              onClick={install}
              className="rounded-lg px-3 py-1.5 text-xs font-semibold text-white"
              style={{ background: "var(--color-primary)" }}
            >
              {t.install.android.action}
            </button>
          )}
          <button
            type="button"
            onClick={dismiss}
            className="text-xs font-medium"
            style={{ color: "var(--color-text-muted)" }}
          >
            {t.install.dismiss}
          </button>
        </div>
      </div>
    </div>
  );
}
