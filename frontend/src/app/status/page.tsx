"use client";

import { useEffect, useState } from "react";
import Image from "next/image";
import { useI18n } from "@/lib/i18n/context";
import { getStatusPage, type StatusPageMonitor } from "@/lib/api";
import { UptimeHistoryBar } from "@/components/UptimeHistoryBar";

export default function StatusPage() {
  const { t, locale } = useI18n();
  const [monitors, setMonitors] = useState<StatusPageMonitor[] | null>(null);
  const [disabled, setDisabled] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = () => {
    getStatusPage()
      .then((data) => {
        setMonitors(data);
        setDisabled(false);
      })
      .catch((err: Error) => {
        if (err.message.toLowerCase().includes("bulunamadi") || err.message.toLowerCase().includes("not found")) {
          setDisabled(true);
        } else {
          setError(err.message);
        }
      });
  };

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, 30_000);
    return () => clearInterval(id);
  }, []);

  const allUp = monitors?.every((m) => m.ok !== false) ?? true;

  return (
    <div className="flex min-h-screen w-full items-start justify-center bg-[var(--color-bg)] px-4 py-16">
      <div className="w-full max-w-xl">
        <div className="mb-8 flex items-center justify-center gap-2">
          <Image src="/logo-mark.png" alt="" width={32} height={32} className="rounded-md" />
          <span className="text-lg font-semibold tracking-tight text-[var(--color-text)]">{t.appName}</span>
        </div>

        {disabled ? (
          <p className="text-center text-sm text-[var(--color-text-muted)]">{t.statusPagePublic.disabled}</p>
        ) : error ? (
          <p className="text-center text-sm text-[var(--color-danger)]">{error}</p>
        ) : monitors === null ? (
          <p className="text-center text-sm text-[var(--color-text-muted)]">{t.dashboard.loading}</p>
        ) : (
          <>
            <div
              className="mb-6 rounded-xl border border-[var(--color-border)] p-4 text-center text-sm font-medium"
              style={{
                color: allUp ? "var(--color-success)" : "var(--color-danger)",
                backgroundColor: allUp
                  ? "color-mix(in srgb, var(--color-success) 12%, transparent)"
                  : "color-mix(in srgb, var(--color-danger) 12%, transparent)",
              }}
            >
              {allUp ? t.statusPagePublic.allOperational : t.statusPagePublic.someDown}
            </div>

            {monitors.length === 0 ? (
              <p className="text-center text-sm text-[var(--color-text-muted)]">{t.statusPagePublic.empty}</p>
            ) : (
              <div className="flex flex-col gap-3">
                {monitors.map((m) => (
                  <div
                    key={m.name}
                    className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] px-4 py-3"
                  >
                    <div className="flex items-center justify-between">
                      <span className="text-sm text-[var(--color-text)]">{m.name}</span>
                      <div className="flex items-center gap-3">
                        {m.uptimeSummary?.day90 !== undefined && (
                          <span className="text-xs text-[var(--color-text-muted)]">
                            %{m.uptimeSummary.day90.toFixed(2)} · {t.monitors.sla.day90}
                          </span>
                        )}
                        <span
                          className="flex items-center gap-1.5 text-xs font-medium"
                          style={{ color: m.ok === false ? "var(--color-danger)" : "var(--color-success)" }}
                        >
                          <span
                            className="h-2 w-2 rounded-full"
                            style={{ backgroundColor: m.ok === false ? "var(--color-danger)" : "var(--color-success)" }}
                          />
                          {m.ok === false ? t.monitors.down : t.monitors.up}
                        </span>
                      </div>
                    </div>
                    {m.dailyHistory?.length > 0 && (
                      <div className="mt-3">
                        <UptimeHistoryBar history={m.dailyHistory} locale={locale === "tr" ? "tr-TR" : "en-GB"} />
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
