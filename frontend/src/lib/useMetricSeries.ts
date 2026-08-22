"use client";

import { useEffect, useState } from "react";
import { queryRange, sharedQueryRange } from "./api";
import type { ChartSeries } from "@/components/MetricChart";

type State = {
  series: ChartSeries | null;
  extraSeries: ChartSeries[];
  loading: boolean;
  error: string | null;
  // False when a host filter is set but this panel's query aggregates the
  // instance label away, so the filter could not narrow anything. Without
  // surfacing this, such a panel looks like it is showing the selected
  // server when it is really showing all of them.
  hostFilterApplied: boolean;
};

// Prometheus is scraped every 15s; asking for a finer step just returns
// repeated points, a coarser one hides short spikes. Keep the step near
// the scrape interval for short windows and widen it for long ones so a
// 7-day view doesn't pull ~40k points per panel.
function stepFor(rangeSeconds: number): string {
  if (rangeSeconds <= 15 * 60) return "15s";
  if (rangeSeconds <= 6 * 3600) return "30s";
  if (rangeSeconds <= 24 * 3600) return "2m";
  if (rangeSeconds <= 3 * 24 * 3600) return "5m";
  return "15m";
}

// A query can legitimately return many series (one per cpu, per mount
// point, per instance...). Name each one after the labels that actually
// *differ* between them — labels they all share carry no information and
// would just produce a legend of identical rows.
function labelsFor(metrics: Record<string, string>[], fallback: string): string[] {
  // One series has nothing to be distinguished from, so the panel's own
  // title is the most useful name for it. Naming it after its labels instead
  // would put "localhost:9100" in the legend of a panel already headed
  // "CPU Usage".
  if (metrics.length === 1) {
    return [fallback || metrics[0].__name__ || describeSingle(metrics[0])];
  }

  const keys = new Set<string>();
  for (const m of metrics) for (const k of Object.keys(m)) if (k !== "__name__") keys.add(k);
  const varying = [...keys].filter((k) => new Set(metrics.map((m) => m[k] ?? "")).size > 1);
  const useKeys = varying.length > 0 ? varying : [...keys];

  const labels = metrics.map((m) => {
    const parts = useKeys.map((k) => m[k]).filter((v) => v !== undefined && v !== "");
    return parts.length > 0 ? parts.join(" · ") : fallback;
  });

  // Last-resort de-duplication so React keys and the legend stay unique
  // even when two series really are labelled identically.
  const seen = new Map<string, number>();
  return labels.map((l) => {
    const n = seen.get(l) ?? 0;
    seen.set(l, n + 1);
    return n === 0 ? l : `${l} (${n + 1})`;
  });
}

function describeSingle(metric: Record<string, string>): string {
  const entries = Object.entries(metric).filter(([k]) => k !== "__name__");
  return entries.map(([, v]) => v).join(" · ");
}

// Cap how many series a single panel renders — a runaway query (e.g. one
// series per container across a cluster) would otherwise lock up the tab.
const MAX_SERIES = 12;

export function useMetricSeries(
  label: string,
  promQL: string,
  rangeSeconds = 3600,
  datasourceId?: string,
  refreshMs = 10_000,
  refreshNonce = 0,
  // Set on a shared dashboard, where there is no session. The share
  // endpoint accepts only that dashboard's own queries, so the panel code
  // stays identical and only the transport changes.
  shareToken?: string,
  // When set, only series carrying this instance label are drawn. Filtering
  // the returned series rather than rewriting the query is deliberate:
  // injecting a label matcher into arbitrary PromQL needs a real parser, and
  // getting that subtly wrong would silently change what a panel measures.
  instanceFilter?: string,
) {
  const [state, setState] = useState<State>({
    series: null,
    extraSeries: [],
    loading: true,
    error: null,
    hostFilterApplied: false,
  });

  useEffect(() => {
    let cancelled = false;

    // Nothing selected yet (e.g. the query builder before a metric is
    // picked) — firing the request anyway just earns a 400 from Prometheus.
    if (!promQL.trim()) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setState({ series: null, extraSeries: [], loading: false, error: null, hostFilterApplied: false });
      return;
    }

    setState((prev) => ({ ...prev, loading: true }));

    const run = () => {
      const now = Math.floor(Date.now() / 1000);
      const step = stepFor(rangeSeconds);
      const request = shareToken
        ? sharedQueryRange(shareToken, promQL, now - rangeSeconds, now, step)
        : queryRange(promQL, now - rangeSeconds, now, step, datasourceId);
      request
        .then((res) => {
          if (cancelled) return;
          let matching = res.result.filter((r) => r.values && r.values.length > 0);
          const carriesInstance = matching.some((r) => r.metric?.instance !== undefined);
          if (instanceFilter) {
            // A query that aggregates the instance label away returns series
            // that carry no instance at all. Those describe every host at
            // once, so they stay visible under any host filter — hiding them
            // would leave the panel blank with no way to tell why.
            matching = matching.filter((r) => {
              const instance = r.metric?.instance;
              return instance === undefined || instance === instanceFilter;
            });
          }
          const withValues = matching.slice(0, MAX_SERIES);
          if (withValues.length === 0) {
            setState({
              series: null,
              extraSeries: [],
              loading: false,
              error: null,
              hostFilterApplied: Boolean(instanceFilter) && carriesInstance,
            });
            return;
          }
          const names = labelsFor(
            withValues.map((r) => r.metric ?? {}),
            label,
          );
          const all: ChartSeries[] = withValues.map((r, i) => ({
            label: names[i],
            timestamps: r.values!.map((v) => v[0]),
            values: r.values!.map((v) => Number(v[1])),
          }));
          setState({
            series: all[0],
            extraSeries: all.slice(1),
            loading: false,
            error: null,
            hostFilterApplied: Boolean(instanceFilter) && carriesInstance,
          });
        })
        .catch((err: Error) => {
          if (!cancelled) {
            setState({
              series: null,
              extraSeries: [],
              loading: false,
              error: err.message,
              hostFilterApplied: false,
            });
          }
        });
    };

    run();

    // Keep monitoring data live: re-poll on an interval instead of only
    // fetching once and going stale until the page is reloaded. A
    // refreshMs of 0 means the user turned auto-refresh off.
    //
    // Nothing is fetched while the tab is in the background. A monitoring
    // dashboard is the kind of page people leave open all day, and every
    // one of those ticks was a range query per panel that nobody could see —
    // paid for by the browser, the backend and Prometheus alike.
    const tick = () => {
      if (document.visibilityState === "hidden") return;
      run();
    };
    const interval = refreshMs > 0 ? setInterval(tick, refreshMs) : null;

    // Coming back to the tab should show current data immediately rather
    // than whatever was on screen when it was hidden, so this fetches at
    // once instead of waiting out the rest of the interval.
    const onVisible = () => {
      if (document.visibilityState === "visible" && refreshMs > 0) run();
    };
    document.addEventListener("visibilitychange", onVisible);

    return () => {
      cancelled = true;
      if (interval) clearInterval(interval);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [label, promQL, rangeSeconds, datasourceId, refreshMs, refreshNonce, shareToken, instanceFilter]);

  return state;
}
