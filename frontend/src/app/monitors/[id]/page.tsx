"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useI18n } from "@/lib/i18n/context";
import { getMonitorDetail, snoozeMonitor, type MonitorCheck, type MonitorDetail, type MonitorFailureClass } from "@/lib/api";
import { LatencySparkline } from "@/components/LatencySparkline";
import { HealthTimeline } from "@/components/HealthTimeline";
import { UptimeHistoryBar, UptimeSummaryRow } from "@/components/UptimeHistoryBar";

// Refreshed on the same cadence as the rest of the app rather than live:
// a check only lands every interval anyway, so anything faster would just
// re-fetch the same rows.
const REFRESH_MS = 30_000;

// A certificate this close to expiry is worth saying out loud. Two weeks
// leaves room to renew by hand if an automated renewal has quietly failed,
// which is the case worth catching — a successful renewal needs no warning.
const CERT_WARNING_DAYS = 14;

function percentile(values: number[], p: number): number {
  if (values.length === 0) return 0;
  const sorted = [...values].sort((a, b) => a - b);
  const index = Math.min(sorted.length - 1, Math.floor((sorted.length - 1) * p));
  return sorted[index];
}

function daysUntil(iso: string): number {
  return Math.floor((new Date(iso).getTime() - Date.now()) / 86_400_000);
}

function isSnoozeActive(until?: string): boolean {
  return !!until && new Date(until).getTime() > Date.now();
}

export default function MonitorDetailPage() {
  const { t, locale } = useI18n();
  const params = useParams<{ id: string }>();
  const id = params?.id;

  const [detail, setDetail] = useState<MonitorDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    if (!id) return;
    try {
      setDetail(await getMonitorDetail(id));
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    // The first fetch has to start somewhere, and this page has no data
    // until it does. The state it sets lands after an await, not during
    // this pass, so it costs no cascading render.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    load();
    // Paused while the tab is hidden, for the same reason the dashboard
    // panels are: nobody is reading a page they cannot see.
    const tick = () => {
      if (document.visibilityState === "visible") load();
    };
    const interval = setInterval(tick, REFRESH_MS);
    document.addEventListener("visibilitychange", tick);
    return () => {
      clearInterval(interval);
      document.removeEventListener("visibilitychange", tick);
    };
  }, [load]);

  const backLink = (
    <Link
      href="/monitors"
      className="inline-flex items-center gap-1.5 text-sm font-medium text-[var(--color-text-muted)] hover:text-[var(--color-text)]"
    >
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M15 18l-6-6 6-6" />
      </svg>
      {t.monitors.backToList}
    </Link>
  );

  if (loading) {
    return (
      <div className="flex flex-col gap-4">
        {backLink}
        <p className="text-sm text-[var(--color-text-muted)]">{t.dashboard.loading}</p>
      </div>
    );
  }

  if (error || !detail) {
    return (
      <div className="flex flex-col gap-4">
        {backLink}
        <p className="text-sm text-[var(--color-danger)]">{error ?? t.monitors.notFound}</p>
      </div>
    );
  }

  const checks = detail.checks;
  const latencies = checks.map((c) => c.latencyMs);
  const failures = checks.filter((c) => !c.ok);
  const lastFailure = failures[0];

  return (
    <div className="flex flex-col gap-4">
      {backLink}

      <Header detail={detail} />

      <SnoozeCard id={detail.id} snoozedUntil={detail.snoozedUntil} onChange={load} />

      {lastFailure && <FailureCard check={lastFailure} />}

      {/* Placed above the rest: with the chat now quiet through short
          stalls, this is the first thing worth seeing on the page. */}
      <Card title={t.monitors.timelineTitle}>
        <HealthTimeline buckets={detail.timeline ?? []} />
      </Card>

      <Card title={t.monitors.sla.title}>
        <UptimeSummaryRow summary={detail.uptimeSummary} />
        {detail.dailyHistory?.length > 0 && (
          <div className="mt-4">
            <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-text-muted)]">
              {t.monitors.sla.historyTitle}
            </p>
            <UptimeHistoryBar history={detail.dailyHistory} locale={locale === "tr" ? "tr-TR" : "en-GB"} />
            <p className="mt-2 text-xs leading-relaxed text-[var(--color-text-muted)]">{t.monitors.sla.historyHint}</p>
          </div>
        )}
      </Card>

      <CertificateCard expiresAt={detail.certExpiresAt} />

      {latencies.length > 0 && (
        <Card title={t.monitors.latencyTitle}>
          <div className="grid grid-cols-3 gap-2">
            <Stat label={t.monitors.latencyMedian} value={`${percentile(latencies, 0.5)} ms`} />
            <Stat label={t.monitors.latencyP95} value={`${percentile(latencies, 0.95)} ms`} />
            <Stat label={t.monitors.latencyWorst} value={`${Math.max(...latencies)} ms`} />
          </div>
          <div className="mt-3">
            <LatencySparkline checks={checks} />
          </div>
          <p className="mt-3 text-xs leading-relaxed text-[var(--color-text-muted)]">{t.monitors.latencyHint}</p>
        </Card>
      )}

      <PhaseCard checks={checks} />

      <Card title={t.monitors.recentFailures}>
        {checks.length === 0 ? (
          <p className="text-sm text-[var(--color-text-muted)]">{t.monitors.checksEmpty}</p>
        ) : failures.length === 0 ? (
          <p className="text-sm text-[var(--color-text-muted)]">{t.monitors.noFailures}</p>
        ) : (
          <ul className="flex flex-col divide-y divide-[var(--color-border)]">
            {failures.slice(0, 10).map((c) => (
              <li key={c.id} className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 py-2 first:pt-0 last:pb-0">
                <span className="text-sm font-medium text-[var(--color-danger)]">
                  {t.monitors.failureClasses[c.errorClass ?? "unknown"].label}
                  {c.statusCode !== undefined && (
                    <span className="ml-1.5 font-mono text-xs text-[var(--color-text-muted)]">HTTP {c.statusCode}</span>
                  )}
                </span>
                <span className="text-xs text-[var(--color-text-muted)]">
                  {new Date(c.checkedAt).toLocaleString(locale === "tr" ? "tr-TR" : "en-GB")}
                </span>
                {c.errorDetail && (
                  <span className="aruzor-code w-full font-mono text-xs text-[var(--color-text-muted)]">{c.errorDetail}</span>
                )}
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}

function Header({ detail }: { detail: MonitorDetail }) {
  const { t, locale } = useI18n();
  const up = detail.lastOk;

  return (
    <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{detail.name}</h2>
        {up === undefined ? (
          <span className="text-xs text-[var(--color-text-muted)]">{t.monitors.pending}</span>
        ) : (
          <span
            className="rounded-full px-2 py-0.5 text-xs font-medium"
            style={{
              color: up ? "var(--color-success)" : "var(--color-danger)",
              backgroundColor: up
                ? "color-mix(in srgb, var(--color-success) 15%, transparent)"
                : "color-mix(in srgb, var(--color-danger) 15%, transparent)",
            }}
          >
            {up ? t.monitors.up : t.monitors.down}
          </span>
        )}
      </div>
      <p className="aruzor-code mt-1 font-mono text-xs text-[var(--color-text-muted)]">
        {detail.type.toUpperCase()} · {detail.target}
      </p>
      <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-3">
        <Stat
          label={t.monitors.columns.uptime}
          value={detail.uptimePercent === undefined ? "—" : `%${detail.uptimePercent.toFixed(2)}`}
        />
        <Stat label={t.monitors.columns.latency} value={detail.lastLatencyMs === undefined ? "—" : `${detail.lastLatencyMs} ms`} />
        <Stat
          label={t.monitors.lastCheck}
          value={
            detail.lastCheckedAt
              ? new Date(detail.lastCheckedAt).toLocaleTimeString(locale === "tr" ? "tr-TR" : "en-GB")
              : "—"
          }
        />
      </div>
    </div>
  );
}

// One-tap maintenance window: pick a duration and down notifications hold
// back until it passes, while the check keeps running underneath — the
// history and the timeline above still show exactly what happened.
function SnoozeCard({ id, snoozedUntil, onChange }: { id: string; snoozedUntil?: string; onChange: () => void }) {
  const { t, locale } = useI18n();
  const [busy, setBusy] = useState(false);

  const active = isSnoozeActive(snoozedUntil);

  const apply = async (minutes: number) => {
    setBusy(true);
    try {
      await snoozeMonitor(id, minutes);
      onChange();
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card title={t.monitors.snooze}>
      {active && snoozedUntil ? (
        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="text-sm text-[var(--color-warning)]">
            🔕 {new Date(snoozedUntil).toLocaleString(locale === "tr" ? "tr-TR" : "en-GB")} {t.monitors.snoozeActive}
          </p>
          <button
            onClick={() => apply(0)}
            disabled={busy}
            className="min-h-9 rounded-md border border-[var(--color-border)] px-3 text-xs font-medium hover:bg-[var(--color-border)]/30 disabled:opacity-60"
          >
            {t.monitors.snoozeClear}
          </button>
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          <p className="text-xs text-[var(--color-text-muted)]">{t.monitors.snoozeHint}</p>
          <div className="flex flex-wrap gap-2">
            {[
              [60, t.monitors.snooze1h],
              [240, t.monitors.snooze4h],
              [1440, t.monitors.snooze24h],
            ].map(([minutes, label]) => (
              <button
                key={minutes}
                onClick={() => apply(minutes as number)}
                disabled={busy}
                className="min-h-9 rounded-md border border-[var(--color-border)] px-3 text-xs font-medium hover:bg-[var(--color-border)]/30 disabled:opacity-60"
              >
                {label}
              </button>
            ))}
          </div>
        </div>
      )}
    </Card>
  );
}

// The failure card is the point of this page: the measured fact on top, the
// checklist below it, and a visible line between them. The list is written
// as things to go and look at, never as a verdict — the prober cannot see
// inside the machine and must not pretend otherwise.
function FailureCard({ check }: { check: MonitorCheck }) {
  const { t, locale } = useI18n();
  const info = t.monitors.failureClasses[(check.errorClass ?? "unknown") as MonitorFailureClass];

  return (
    <div className="overflow-hidden rounded-xl border border-[var(--color-danger)]/40 bg-[var(--color-card)]">
      <div className="bg-[var(--color-danger)]/10 px-4 py-3">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <span className="text-sm font-semibold text-[var(--color-danger)]">{info.label}</span>
          <span className="text-xs text-[var(--color-text-muted)]">
            {new Date(check.checkedAt).toLocaleString(locale === "tr" ? "tr-TR" : "en-GB")}
          </span>
        </div>
        <p className="mt-1 text-sm leading-relaxed text-[var(--color-text)]">{info.means}</p>
        {check.errorDetail && (
          <p className="aruzor-code mt-2 font-mono text-xs text-[var(--color-text-muted)]">
            {t.monitors.measured}: {check.errorDetail}
          </p>
        )}
      </div>
      <div className="px-4 py-3">
        <p className="text-xs font-semibold uppercase tracking-wide text-[var(--color-text-muted)]">
          {t.monitors.whatToCheck}
        </p>
        <ul className="mt-2 flex flex-col gap-2">
          {info.checks.map((item) => (
            <li key={item} className="flex gap-2 text-sm leading-relaxed">
              <span aria-hidden="true" className="mt-2 h-1.5 w-1.5 shrink-0 rounded-full bg-[var(--color-text-muted)]" />
              <span>{item}</span>
            </li>
          ))}
        </ul>
        <p className="mt-3 text-xs leading-relaxed text-[var(--color-text-muted)]">{t.monitors.whatToCheckHint}</p>
      </div>
    </div>
  );
}

function CertificateCard({ expiresAt }: { expiresAt?: string }) {
  const { t, locale } = useI18n();
  if (!expiresAt) {
    return (
      <Card title={t.monitors.certificate}>
        <p className="text-sm text-[var(--color-text-muted)]">{t.monitors.certUnknown}</p>
      </Card>
    );
  }

  const days = daysUntil(expiresAt);
  const expired = days < 0;
  const soon = days >= 0 && days <= CERT_WARNING_DAYS;
  const color = expired || soon ? "var(--color-warning)" : "var(--color-text)";

  return (
    <Card title={t.monitors.certificate}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="text-sm text-[var(--color-text-muted)]">{t.monitors.certValidUntil}</span>
        <span className="text-sm font-medium" style={{ color }}>
          {new Date(expiresAt).toLocaleDateString(locale === "tr" ? "tr-TR" : "en-GB")}
          {!expired && ` · ${days} ${t.monitors.certDaysLeft}`}
        </span>
      </div>
      {(expired || soon) && (
        <p className="mt-2 text-xs leading-relaxed text-[var(--color-warning)]">
          {expired ? t.monitors.certExpired : t.monitors.certExpiringSoon}
        </p>
      )}
    </Card>
  );
}

// Phase timings answer "which layer is slow" without any inference, so they
// are worth showing even when everything is healthy — that is the baseline
// you compare against on the day it is not.
function PhaseCard({ checks }: { checks: MonitorCheck[] }) {
  const { t } = useI18n();
  const withPhases = checks.filter((c) => c.connectMs !== undefined);
  if (withPhases.length === 0) return null;

  const connect = percentile(withPhases.map((c) => c.connectMs ?? 0), 0.5);
  const tlsSamples = withPhases.filter((c) => c.tlsMs !== undefined).map((c) => c.tlsMs ?? 0);
  const tls = tlsSamples.length > 0 ? percentile(tlsSamples, 0.5) : null;
  const total = percentile(withPhases.map((c) => c.latencyMs), 0.5);
  // Whatever is left after connecting and negotiating TLS is the server
  // thinking. Clamped at zero because the three medians are taken over the
  // same window but not necessarily the same checks.
  const response = Math.max(0, total - connect - (tls ?? 0));

  // Not primary/secondary: those two resolve to the same hex in the light
  // theme, which made the first two segments of this bar indistinguishable.
  // A tint of one colour plus the warning tone stays legible in both themes.
  const segments = [
    { label: t.monitors.phaseConnect, value: connect, color: "color-mix(in srgb, var(--color-primary) 40%, transparent)" },
    ...(tls !== null ? [{ label: t.monitors.phaseTLS, value: tls, color: "var(--color-primary)" }] : []),
    { label: t.monitors.phaseResponse, value: response, color: "var(--color-warning)" },
  ];
  const sum = segments.reduce((acc, s) => acc + s.value, 0) || 1;

  return (
    <Card title={t.monitors.phaseTitle}>
      <div className="flex h-2.5 w-full overflow-hidden rounded-full bg-[var(--color-border)]">
        {segments.map((s) => (
          <div key={s.label} style={{ width: `${(s.value / sum) * 100}%`, backgroundColor: s.color }} />
        ))}
      </div>
      <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1">
        {segments.map((s) => (
          <span key={s.label} className="flex items-center gap-1.5 text-xs">
            <span aria-hidden="true" className="h-2 w-2 rounded-sm" style={{ backgroundColor: s.color }} />
            <span className="text-[var(--color-text-muted)]">{s.label}</span>
            <span className="font-medium tabular-nums">{s.value} ms</span>
          </span>
        ))}
      </div>
      <p className="mt-3 text-xs leading-relaxed text-[var(--color-text-muted)]">{t.monitors.phaseHint}</p>
    </Card>
  );
}

function Card({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <h3 className="mb-3 text-sm font-semibold">{title}</h3>
      {children}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="truncate text-xs text-[var(--color-text-muted)]">{label}</div>
      <div className="truncate text-base font-semibold tabular-nums">{value}</div>
    </div>
  );
}
