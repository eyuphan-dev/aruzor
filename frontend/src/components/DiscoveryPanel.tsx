"use client";

import { useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n/context";
import { discoverIntegrations, type DiscoveryResult, type DiscoveredIntegration } from "@/lib/api";

// The first thing anyone sees on a fresh install used to be an empty grid
// and a PromQL box — the single most cited reason people bounce off
// self-hosted monitoring. This asks the connected Prometheus what it is
// already scraping and offers those panels directly, so a new server goes
// from install to a working dashboard without anyone writing a query.
export function DiscoveryPanel({
  datasourceId,
  onAdd,
  compact = false,
}: {
  datasourceId?: string;
  onAdd: (integration: DiscoveredIntegration) => void;
  compact?: boolean;
}) {
  const { t, locale } = useI18n();
  const [result, setResult] = useState<DiscoveryResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    discoverIntegrations(datasourceId)
      .then((r) => {
        if (!cancelled) {
          setResult(r);
          setLoading(false);
        }
      })
      .catch((err: Error) => {
        if (!cancelled) {
          setError(err.message);
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [datasourceId]);

  if (loading) {
    return (
      <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-6 text-center text-sm text-[var(--color-text-muted)]">
        {t.discovery.scanning}
      </div>
    );
  }

  if (error || !result) {
    return (
      <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-6 text-center text-sm text-[var(--color-danger)]">
        {error ?? t.dashboard.noData}
      </div>
    );
  }

  // Three outcomes worth telling apart: Prometheus can't be reached at all
  // (a setup problem), it answers but scrapes nothing recognisable (nothing
  // to suggest yet), and the normal case.
  if (!result.prometheusReachable) {
    return (
      <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-6">
        <p className="text-sm font-medium text-[var(--color-danger)]">{t.discovery.unreachableTitle}</p>
        <p className="mt-2 text-sm text-[var(--color-text-muted)]">{t.discovery.unreachableHint}</p>
      </div>
    );
  }

  if (result.integrations.length === 0) {
    return (
      <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-6">
        <p className="text-sm font-medium text-[var(--color-text)]">{t.discovery.nothingTitle}</p>
        <p className="mt-2 text-sm text-[var(--color-text-muted)]">
          {t.discovery.nothingHint.replace("{count}", String(result.metricCount))}
        </p>
      </div>
    );
  }

  return (
    <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-5">
      <p className="text-sm font-semibold text-[var(--color-text)]">{t.discovery.foundTitle}</p>
      {!compact && <p className="mt-1 text-sm text-[var(--color-text-muted)]">{t.discovery.foundHint}</p>}

      <div className="mt-4 flex flex-col gap-2">
        {result.integrations.map((integration) => (
          <div
            key={integration.id}
            className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-[var(--color-border)] px-4 py-3"
          >
            <div className="min-w-0">
              <p className="truncate text-sm font-medium text-[var(--color-text)]">
                {locale === "tr" ? integration.nameTr : integration.nameEn}
              </p>
              <p className="text-xs text-[var(--color-text-muted)]">
                {t.discovery.panelCount.replace("{count}", String(integration.panels.length))}
              </p>
            </div>
            <button
              onClick={() => onAdd(integration)}
              className="min-h-10 shrink-0 rounded-lg bg-[var(--color-primary)] px-4 text-sm font-medium text-white hover:opacity-90"
            >
              {t.discovery.add}
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
