"use client";

import { useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n/context";
import { getMetricNames, listDatasources, type Datasource } from "@/lib/api";
import { useMetricSeries } from "@/lib/useMetricSeries";
import { PanelCard } from "@/components/PanelCard";
import { MetricChart } from "@/components/MetricChart";
import { HelpTooltip } from "@/components/HelpTooltip";

const RANGE_OPTIONS = [
  { label: "15m", seconds: 900 },
  { label: "1h", seconds: 3600 },
  { label: "6h", seconds: 21600 },
  { label: "24h", seconds: 86400 },
];

const QUERY_HISTORY_STORAGE_KEY = "aruzor-query-history";
const QUERY_HISTORY_LIMIT = 10;

function loadQueryHistory(): string[] {
  if (typeof window === "undefined") return [];
  try {
    const raw = window.localStorage.getItem(QUERY_HISTORY_STORAGE_KEY);
    const parsed = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed.filter((v) => typeof v === "string") : [];
  } catch {
    return [];
  }
}

export default function QueryBuilderPage() {
  const { t } = useI18n();
  const [datasources, setDatasources] = useState<Datasource[]>([]);
  const [selectedDatasource, setSelectedDatasource] = useState<string>("default");
  const [metrics, setMetrics] = useState<string[]>([]);
  const [selectedMetric, setSelectedMetric] = useState<string>("");
  const [rangeSeconds, setRangeSeconds] = useState(RANGE_OPTIONS[1].seconds);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [queryHistory, setQueryHistory] = useState<string[]>([]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setQueryHistory(loadQueryHistory());
  }, []);

  useEffect(() => {
    listDatasources()
      .then(setDatasources)
      .catch(() => setDatasources([]));
  }, []);

  useEffect(() => {
    getMetricNames(selectedDatasource)
      .then((names) => {
        setMetrics(names);
        setSelectedMetric((prev) => (names.includes(prev) ? prev : names[0] || ""));
      })
      .catch((err: Error) => setFetchError(err.message));
  }, [selectedDatasource]);

  const selectMetric = (metric: string) => {
    setSelectedMetric(metric);
    if (!metric) return;
    setQueryHistory((prev) => {
      const next = [metric, ...prev.filter((m) => m !== metric)].slice(0, QUERY_HISTORY_LIMIT);
      window.localStorage.setItem(QUERY_HISTORY_STORAGE_KEY, JSON.stringify(next));
      return next;
    });
  };

  const promQL = selectedMetric ? `${selectedMetric}` : "";
  const { series, loading, error } = useMetricSeries(selectedMetric, promQL, rangeSeconds, selectedDatasource);

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="aruzor-page-title flex items-center gap-2 text-xl font-semibold">
          {t.queryBuilder.title}
          <HelpTooltip text={t.queryBuilder.helpText} />
        </h1>
        <p className="text-sm text-[var(--color-text-muted)]">{t.queryBuilder.subtitle}</p>
      </div>

      <div className="flex flex-wrap items-end gap-4 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
        {datasources.length > 1 && (
          <div className="flex flex-col gap-1">
            <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.datasources.title}</label>
            <select
              value={selectedDatasource}
              onChange={(e) => setSelectedDatasource(e.target.value)}
              className="aruzor-select min-w-[180px]"
            >
              {datasources.map((ds) => (
                <option key={ds.id} value={ds.id}>
                  {ds.name}
                </option>
              ))}
            </select>
          </div>
        )}

        <div className="flex flex-col gap-1">
          <label className="text-xs font-medium text-[var(--color-text-muted)]">
            {t.queryBuilder.metricLabel}
          </label>
          <select
            value={selectedMetric}
            onChange={(e) => selectMetric(e.target.value)}
            className="aruzor-select min-w-[220px]"
          >
            {metrics.length === 0 && <option value="">{t.queryBuilder.metricPlaceholder}</option>}
            {metrics.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
        </div>

        <div className="flex flex-col gap-1">
          <label className="text-xs font-medium text-[var(--color-text-muted)]">
            {t.queryBuilder.rangeLabel}
          </label>
          <select
            value={rangeSeconds}
            onChange={(e) => setRangeSeconds(Number(e.target.value))}
            className="aruzor-select"
          >
            {RANGE_OPTIONS.map((r) => (
              <option key={r.label} value={r.seconds}>
                {r.label}
              </option>
            ))}
          </select>
        </div>
      </div>

      {queryHistory.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs font-medium text-[var(--color-text-muted)]">{t.queryBuilder.recentQueries}</span>
          {queryHistory.map((m) => (
            <button
              key={m}
              onClick={() => selectMetric(m)}
              className={`rounded-full border px-2.5 py-1 text-xs ${
                m === selectedMetric
                  ? "border-[var(--color-primary)] text-[var(--color-primary)]"
                  : "border-[var(--color-border)] text-[var(--color-text-muted)] hover:bg-[var(--color-border)]/30"
              }`}
            >
              {m}
            </button>
          ))}
        </div>
      )}

      <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
        <div className="mb-2 text-xs font-medium text-[var(--color-text-muted)]">
          {t.queryBuilder.generatedQuery}
        </div>
        <code className="aruzor-code block rounded-md bg-[var(--color-bg)] px-3 py-2 text-sm text-[var(--color-primary)]">
          {promQL || "—"}
        </code>
      </div>

      <PanelCard title={selectedMetric || t.queryBuilder.metricPlaceholder} status={error || fetchError ? "error" : "ok"}>
        {loading ? (
          <div className="flex h-[220px] items-center justify-center text-sm text-[var(--color-text-muted)]">
            {t.dashboard.loading}
          </div>
        ) : error || fetchError || !series ? (
          <div className="flex h-[220px] items-center justify-center text-center text-sm text-[var(--color-text-muted)]">
            {fetchError ?? t.dashboard.noData}
          </div>
        ) : (
          // uPlot draws into the box it is given, and this card has no height
          // of its own to hand down — the chart came out zero pixels tall on
          // every screen, so the page never showed its own result. The two
          // states above already declare 220px; matching it keeps the card
          // from resizing as data arrives.
          <div className="h-[220px] sm:h-[320px]">
            <MetricChart series={series} />
          </div>
        )}
      </PanelCard>
    </div>
  );
}
