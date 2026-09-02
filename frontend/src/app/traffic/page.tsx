"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useI18n } from "@/lib/i18n/context";
import { useAuth } from "@/lib/auth/context";
import {
  getTraffic,
  getTrafficRequests,
  type TrafficOverview,
  type TrafficRange,
  type TrafficRequestRow,
} from "@/lib/api";
import { MetricChart, type ChartSeries } from "@/components/MetricChart";
import { PanelCard } from "@/components/PanelCard";
import { TrafficRanking } from "@/components/TrafficRanking";
import { seriesColor } from "@/lib/chartColors";
import { formatBytes, formatBytesPerSecond, formatCount, formatDateTime, formatRate } from "@/lib/format";

const RANGES: TrafficRange[] = ["1h", "6h", "24h", "7d"];

type RequestFilter = "all" | "unauthorized" | "errors";

// The page polls, because a request-rate chart that only updates on reload
// is a screenshot. A minute matches the collector's own flush interval —
// polling faster would re-render the same numbers.
const REFRESH_MS = 60_000;

// The log format that fills every panel. Offered as text to copy rather
// than applied by Aruzor: it is the operator's web server config, and a
// monitoring tool that rewrites nginx.conf behind their back is not a
// monitoring tool.
const SUGGESTED_LOG_FORMAT = `log_format aruzor '$remote_addr - $remote_user [$time_local] "$request" '
                  '$status $body_bytes_sent "$http_referer" "$http_user_agent" '
                  '"$host" $request_time "$upstream_addr"';

access_log /www/wwwlogs/access.log aruzor;`;

export default function TrafficPage() {
  const { t } = useI18n();
  const { session } = useAuth();

  const [range, setRange] = useState<TrafficRange>("24h");
  const [data, setData] = useState<TrafficOverview | null>(null);
  const [error, setError] = useState<string | null>(null);

  const [requestFilter, setRequestFilter] = useState<RequestFilter>("all");
  // The filter is stored alongside the rows it produced, so switching
  // filters cannot briefly show one filter's results under another's
  // heading while the new request is still in flight.
  const [filtered, setFiltered] = useState<{
    filter: RequestFilter;
    range: TrafficRange;
    rows: TrafficRequestRow[];
  } | null>(null);

  const load = useCallback(
    (r: TrafficRange) =>
      getTraffic(r)
        .then((res) => {
          setData(res);
          setError(null);
        })
        .catch((err: Error) => setError(err.message)),
    [],
  );

  useEffect(() => {
    load(range);
    const timer = setInterval(() => load(range), REFRESH_MS);
    return () => clearInterval(timer);
  }, [range, load]);

  // The overview already carries the newest requests, so the default view
  // costs one call; only a filter needs a second one.
  useEffect(() => {
    if (requestFilter === "all") return;
    let cancelled = false;
    getTrafficRequests(requestFilter, range)
      .then((rows) => {
        if (!cancelled) setFiltered({ filter: requestFilter, range, rows });
      })
      .catch(() => {
        if (!cancelled) setFiltered({ filter: requestFilter, range, rows: [] });
      });
    return () => {
      cancelled = true;
    };
  }, [requestFilter, range]);

  // Derived rather than tracked: the response says which range it answers,
  // so "still loading" is simply "what is on screen is not what was asked
  // for" — one less piece of state to keep in sync with the fetches.
  const loading = data === null || data.range !== range;

  const rateSeries = useMemo(() => buildRateSeries(data, t.traffic.panels.requestRate), [data, t]);
  const egressSeries = useMemo(() => buildEgressSeries(data, t.traffic.panels.egress), [data, t]);

  if (session && session.role !== "admin" && session.role !== "super_admin") {
    return <p className="text-sm text-[var(--color-text-muted)]">{t.common.forbidden}</p>;
  }

  const shownRequests =
    requestFilter === "all"
      ? (data?.recent ?? [])
      : filtered?.filter === requestFilter && filtered.range === range
        ? filtered.rows
        : [];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h1 className="aruzor-page-title text-xl font-semibold">{t.traffic.title}</h1>
          <p className="text-sm text-[var(--color-text-muted)]">{t.traffic.subtitle}</p>
        </div>
        <div className="flex flex-wrap items-center gap-1 rounded-lg border border-[var(--color-border)] p-1">
          {RANGES.map((r) => (
            <button
              key={r}
              onClick={() => setRange(r)}
              aria-pressed={range === r}
              className={`min-h-9 rounded-md px-3 py-1.5 text-xs font-medium ${
                range === r
                  ? "bg-[var(--color-primary)]/15 text-[var(--color-primary)]"
                  : "text-[var(--color-text-muted)] hover:text-[var(--color-text)]"
              }`}
            >
              {t.traffic.ranges[r]}
            </button>
          ))}
        </div>
      </div>

      {error && <p className="text-sm text-[var(--color-danger)]">{error}</p>}
      {loading && !data && <p className="text-sm text-[var(--color-text-muted)]">{t.dashboard.loading}</p>}

      {data && !data.enabled && (
        <SetupCard title={t.traffic.disabledTitle} body={t.traffic.disabledBody}>
          <p className="mt-3 text-xs font-medium text-[var(--color-text-muted)]">{t.traffic.disabledExample}</p>
          <pre className="aruzor-code mt-1 rounded-lg bg-[var(--color-border)]/30 p-3 text-xs">
            ARUZOR_ACCESS_LOG_PATHS=/www/wwwlogs/*.log
          </pre>
        </SetupCard>
      )}

      {data && data.enabled && !data.hasData && (
        <SetupCard title={t.traffic.emptyTitle} body={t.traffic.emptyBody}>
          <p className="mt-3 text-xs font-medium text-[var(--color-text-muted)]">{t.traffic.configuredPaths}</p>
          <pre className="aruzor-code mt-1 rounded-lg bg-[var(--color-border)]/30 p-3 text-xs">
            {data.configuredPaths.join("\n") || "—"}
          </pre>
        </SetupCard>
      )}

      {data && data.hasData && (
        <>
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <StatTile
              label={t.traffic.panels.requestRate}
              value={`${formatRate(data.totals.requestsPerSecond)}/s`}
              hint={`${t.traffic.stats.peak} ${formatRate(data.totals.peakRequestsPerSecond)}/s · ${t.traffic.stats.total} ${formatCount(data.totals.requests)}`}
            />
            <StatTile
              label={t.traffic.panels.egress}
              value={formatBytesPerSecond(data.totals.bytesPerSecond)}
              hint={`${t.traffic.stats.total} ${formatBytes(data.totals.bytes)}`}
            />
            <StatTile
              label={t.traffic.stats.errorRate}
              value={`%${data.totals.errorRate.toFixed(2)}`}
              hint={`${formatCount(data.totals.errors5xx)} × 5xx · ${formatCount(data.totals.errors4xx)} × 4xx`}
              tone={data.totals.errors5xx > 0 ? "danger" : undefined}
            />
            <StatTile
              label={t.traffic.stats.unauthorized}
              value={formatCount(data.totals.unauthorized)}
              hint={
                data.totals.hasDuration
                  ? `${t.traffic.stats.avgDuration} ${Math.round(data.totals.avgDurationMs)} ms`
                  : t.traffic.notes.unauthorized
              }
              tone={data.totals.unauthorized > 0 ? "warn" : undefined}
            />
          </div>

          <div className="grid gap-4 xl:grid-cols-2">
            <PanelCard title={t.traffic.panels.requestRate}>
              <div className="h-56">
                <MetricChart series={rateSeries.main} extraSeries={rateSeries.extra} kind="line" />
              </div>
            </PanelCard>
            <PanelCard title={t.traffic.panels.egress}>
              <div className="h-56">
                <MetricChart series={egressSeries} kind="area" color={seriesColor(3)} />
              </div>
            </PanelCard>
          </div>

          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            <PanelCard title={t.traffic.panels.topIps}>
              <TrafficRanking rows={data.topIps} secondary="bytes" emptyLabel={t.traffic.empty} />
            </PanelCard>
            <PanelCard title={t.traffic.panels.topIpsByBytes}>
              <TrafficRanking rows={data.topIpsByBytes} metric="bytes" secondary="requests" emptyLabel={t.traffic.empty} />
            </PanelCard>
            <PanelCard title={t.traffic.panels.topPaths}>
              <TrafficRanking rows={data.topPaths} emptyLabel={t.traffic.empty} note={t.traffic.notes.paths} />
            </PanelCard>
            <PanelCard title={t.traffic.panels.clients}>
              <TrafficRanking rows={data.topClients} emptyLabel={t.traffic.empty} note={t.traffic.notes.clients} />
            </PanelCard>
            <PanelCard title={t.traffic.panels.statusCodes}>
              <TrafficRanking rows={data.statusCodes} emptyLabel={t.traffic.empty} />
            </PanelCard>
            <PanelCard title={t.traffic.panels.methods}>
              <TrafficRanking rows={data.methods} emptyLabel={t.traffic.empty} />
            </PanelCard>

            {/* Host and service depend on log_format fields the stock
                combined format does not carry. An empty chart would look
                like "no traffic"; the note says what to change instead. */}
            <PanelCard title={t.traffic.panels.hosts}>
              {data.fields.host ? (
                <TrafficRanking rows={data.topHosts} secondary="bytes" emptyLabel={t.traffic.empty} />
              ) : (
                <FieldMissing message={t.traffic.hostMissing} />
              )}
            </PanelCard>
            <PanelCard title={t.traffic.panels.services}>
              {data.fields.service ? (
                <TrafficRanking rows={data.topServices} secondary="bytes" emptyLabel={t.traffic.empty} />
              ) : (
                <FieldMissing message={t.traffic.serviceMissing} />
              )}
            </PanelCard>
            <PanelCard title={t.traffic.panels.nodes}>
              <TrafficRanking rows={data.nodes} secondary="bytes" emptyLabel={t.traffic.empty} />
            </PanelCard>
          </div>

          <div className="grid gap-4 xl:grid-cols-2">
            <PanelCard title={t.traffic.panels.errorPaths}>
              <TrafficRanking rows={data.errorPaths} metric="errors" secondary="requests" emptyLabel={t.traffic.empty} />
            </PanelCard>
            <PanelCard title={t.traffic.panels.unauthorized}>
              {data.unauthorized.length === 0 ? (
                <p className="py-6 text-center text-sm text-[var(--color-text-muted)]">{t.traffic.empty}</p>
              ) : (
                <RequestTable rows={data.unauthorized} compact />
              )}
            </PanelCard>
          </div>

          <div className="flex flex-col gap-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h2 className="text-sm font-medium">{t.traffic.panels.recent}</h2>
              <div className="flex items-center gap-1 rounded-lg border border-[var(--color-border)] p-1">
                {(["all", "unauthorized", "errors"] as RequestFilter[]).map((f) => (
                  <button
                    key={f}
                    onClick={() => setRequestFilter(f)}
                    aria-pressed={requestFilter === f}
                    className={`min-h-9 rounded-md px-3 py-1.5 text-xs font-medium ${
                      requestFilter === f
                        ? "bg-[var(--color-primary)]/15 text-[var(--color-primary)]"
                        : "text-[var(--color-text-muted)] hover:text-[var(--color-text)]"
                    }`}
                  >
                    {t.traffic.filters[f]}
                  </button>
                ))}
              </div>
            </div>
            <RequestTable rows={shownRequests} emptyLabel={t.traffic.empty} />
          </div>

          <SourcesPanel data={data} />
        </>
      )}
    </div>
  );
}

function StatTile({
  label,
  value,
  hint,
  tone,
}: {
  label: string;
  value: string;
  hint?: string;
  tone?: "warn" | "danger";
}) {
  const color =
    tone === "danger" ? "var(--color-danger)" : tone === "warn" ? "var(--color-warning)" : "var(--color-text)";
  return (
    <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <p className="truncate text-xs text-[var(--color-text-muted)]">{label}</p>
      {/* Two tiles share a 360px row, so the figure steps down a size on a
          phone rather than wrapping onto a second line and pushing the hint
          out of the card. */}
      <p className="mt-1 text-xl font-semibold tabular-nums sm:text-2xl" style={{ color }}>
        {value}
      </p>
      {hint && <p className="mt-1 text-[11px] leading-snug text-[var(--color-text-muted)]">{hint}</p>}
    </div>
  );
}

function SetupCard({ title, body, children }: { title: string; body: string; children?: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-5">
      <h2 className="text-sm font-semibold">{title}</h2>
      <p className="mt-1 max-w-2xl text-sm text-[var(--color-text-muted)]">{body}</p>
      {children}
    </div>
  );
}

function FieldMissing({ message }: { message: string }) {
  return (
    <div className="flex h-full items-center">
      <p className="text-xs leading-relaxed text-[var(--color-text-muted)]">{message}</p>
    </div>
  );
}

function RequestTable({
  rows,
  compact,
  emptyLabel,
}: {
  rows: TrafficRequestRow[];
  compact?: boolean;
  emptyLabel?: string;
}) {
  const { t } = useI18n();

  if (rows.length === 0) {
    return <p className="py-6 text-center text-sm text-[var(--color-text-muted)]">{emptyLabel ?? t.traffic.empty}</p>;
  }

  return (
    <div className="aruzor-table-wrap overflow-x-auto rounded-xl border border-[var(--color-border)] bg-[var(--color-card)]">
      <table className="aruzor-table w-full min-w-[640px] text-left text-sm">
        <thead>
          <tr className="border-b border-[var(--color-border)] text-xs text-[var(--color-text-muted)]">
            <th className="px-3 py-2 font-medium">{t.traffic.columns.time}</th>
            <th className="px-3 py-2 font-medium">{t.traffic.columns.ip}</th>
            <th className="px-3 py-2 font-medium">{t.traffic.columns.method}</th>
            <th className="px-3 py-2 font-medium">{t.traffic.columns.path}</th>
            <th className="px-3 py-2 font-medium">{t.traffic.columns.status}</th>
            {!compact && <th className="px-3 py-2 font-medium">{t.traffic.columns.bytes}</th>}
            {!compact && <th className="px-3 py-2 font-medium">{t.traffic.columns.client}</th>}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.id} className="border-b border-[var(--color-border)] last:border-0">
              <td className="px-3 py-2 whitespace-nowrap text-[var(--color-text-muted)]" data-label={t.traffic.columns.time}>
                {formatDateTime(r.at)}
              </td>
              <td className="px-3 py-2 font-mono text-[13px]" data-label={t.traffic.columns.ip}>
                {r.ip}
              </td>
              <td className="px-3 py-2 text-xs" data-label={t.traffic.columns.method}>
                {r.method}
              </td>
              <td className="px-3 py-2" data-label={t.traffic.columns.path}>
                <span className="aruzor-code font-mono text-[13px]" title={r.host ? `${r.host}${r.path}` : r.path}>
                  {r.path}
                </span>
              </td>
              <td className="px-3 py-2" data-label={t.traffic.columns.status}>
                <StatusBadge status={r.status} />
              </td>
              {!compact && (
                <td className="px-3 py-2 tabular-nums text-[var(--color-text-muted)]" data-label={t.traffic.columns.bytes}>
                  {formatBytes(r.bytes)}
                </td>
              )}
              {!compact && (
                <td
                  className="max-w-[220px] px-3 py-2 text-xs text-[var(--color-text-muted)]"
                  data-label={t.traffic.columns.client}
                >
                  <span className="block truncate" title={r.userAgent}>
                    {r.userAgent || "—"}
                  </span>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function StatusBadge({ status }: { status: number }) {
  const tone =
    status >= 500
      ? "bg-[var(--color-danger)]/15 text-[var(--color-danger)]"
      : status === 401 || status === 403
        ? "bg-[var(--color-warning)]/15 text-[var(--color-warning)]"
        : status >= 400
          ? "text-[var(--color-text-muted)]"
          : "bg-[var(--color-success)]/15 text-[var(--color-success)]";
  return <span className={`rounded-full px-2 py-0.5 text-xs font-medium tabular-nums ${tone}`}>{status}</span>;
}

function SourcesPanel({ data }: { data: TrafficOverview }) {
  const { t } = useI18n();
  const unparsed = data.sources.reduce((sum, s) => sum + s.unparsed, 0);
  const lines = data.sources.reduce((sum, s) => sum + s.lines, 0);
  // One in twenty lines failing is noise; a fifth of them failing means the
  // log_format is not what the parser expects, and that is worth saying.
  const formatLooksWrong = lines > 0 && unparsed / (lines + unparsed) > 0.2;

  return (
    <details className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <summary className="cursor-pointer text-sm font-medium">{t.traffic.sourcesTitle}</summary>

      <div className="mt-3 aruzor-table-wrap overflow-x-auto">
        <table className="aruzor-table w-full min-w-[560px] text-left text-sm">
          <thead>
            <tr className="border-b border-[var(--color-border)] text-xs text-[var(--color-text-muted)]">
              <th className="px-3 py-2 font-medium">{t.traffic.sourcesColumns.name}</th>
              <th className="px-3 py-2 font-medium">{t.traffic.sourcesColumns.path}</th>
              <th className="px-3 py-2 font-medium">{t.traffic.sourcesColumns.lines}</th>
              <th className="px-3 py-2 font-medium">{t.traffic.sourcesColumns.unparsed}</th>
              <th className="px-3 py-2 font-medium">{t.traffic.sourcesColumns.lastRead}</th>
            </tr>
          </thead>
          <tbody>
            {data.sources.map((s) => (
              <tr key={s.id} className="border-b border-[var(--color-border)] last:border-0">
                <td className="px-3 py-2" data-label={t.traffic.sourcesColumns.name}>
                  {s.name}
                </td>
                <td className="px-3 py-2 text-xs text-[var(--color-text-muted)]" data-label={t.traffic.sourcesColumns.path}>
                  <span className="aruzor-code">{s.path}</span>
                </td>
                <td className="px-3 py-2 tabular-nums" data-label={t.traffic.sourcesColumns.lines}>
                  {formatCount(s.lines)}
                </td>
                <td className="px-3 py-2 tabular-nums" data-label={t.traffic.sourcesColumns.unparsed}>
                  {formatCount(s.unparsed)}
                </td>
                <td
                  className="px-3 py-2 whitespace-nowrap text-[var(--color-text-muted)]"
                  data-label={t.traffic.sourcesColumns.lastRead}
                >
                  {s.lastReadAt ? formatDateTime(s.lastReadAt) : "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {formatLooksWrong && <p className="mt-2 text-xs text-[var(--color-warning)]">{t.traffic.unparsedHint}</p>}

      {(!data.fields.host || !data.fields.service || !data.fields.duration) && (
        <div className="mt-4">
          <p className="text-sm font-medium">{t.traffic.logFormatTitle}</p>
          <p className="mt-1 max-w-2xl text-xs text-[var(--color-text-muted)]">{t.traffic.logFormatHint}</p>
          <pre className="aruzor-code mt-2 overflow-x-auto rounded-lg bg-[var(--color-border)]/30 p-3 text-xs">
            {SUGGESTED_LOG_FORMAT}
          </pre>
        </div>
      )}
    </details>
  );
}

// A counter divided by the step is a rate; the API sends counts per step so
// the same series can also be summed, and the division belongs wherever the
// step is known rather than being baked into the payload.
function buildRateSeries(data: TrafficOverview | null, label: string): { main: ChartSeries | null; extra: ChartSeries[] } {
  if (!data || data.series.length === 0) return { main: null, extra: [] };
  const step = data.stepSeconds || 60;
  const timestamps = data.series.map((p) => p.at);
  return {
    main: { label, timestamps, values: data.series.map((p) => p.requests / step) },
    extra: [
      { label: "4xx", timestamps, values: data.series.map((p) => p.s4xx / step) },
      { label: "5xx", timestamps, values: data.series.map((p) => p.s5xx / step) },
    ],
  };
}

function buildEgressSeries(data: TrafficOverview | null, label: string): ChartSeries | null {
  if (!data || data.series.length === 0) return null;
  const step = data.stepSeconds || 60;
  return {
    label,
    timestamps: data.series.map((p) => p.at),
    values: data.series.map((p) => p.bytes / step),
  };
}
