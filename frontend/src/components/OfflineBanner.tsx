"use client";

import { useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n/context";
import { isReachable, onReachabilityChange } from "@/lib/api";

// Installed on a phone, this app will regularly be opened with no signal.
// Without a banner, every panel just reports "no data — check your Prometheus
// connection", which sends the reader to debug a server that is fine. Saying
// plainly that the connection is down is both true and far more useful.
//
// Two signals, because neither alone is sufficient: navigator.onLine flips
// instantly when the radio drops but stays true on a captive portal or dead
// Wi-Fi, while a failed request proves nothing is getting through but only
// after something has been tried.
export function OfflineBanner() {
  const { t } = useI18n();
  const [down, setDown] = useState(false);

  useEffect(() => {
    const update = () => setDown(!navigator.onLine || !isReachable());

    // Read after mount: neither signal exists on the server, and the
    // server-rendered markup has to match the first client paint.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    update();

    window.addEventListener("online", update);
    window.addEventListener("offline", update);
    const unsubscribe = onReachabilityChange(update);
    return () => {
      window.removeEventListener("online", update);
      window.removeEventListener("offline", update);
      unsubscribe();
    };
  }, []);

  if (!down) return null;

  return (
    <div
      role="status"
      className="aruzor-app-banner bg-[var(--color-warning)]/15 text-center text-xs text-[var(--color-warning)]"
    >
      {t.common.offline}
    </div>
  );
}
