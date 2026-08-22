"use client";

import { useI18n } from "@/lib/i18n/context";
import type { DailyUptime, UptimeSummary } from "@/lib/api";

// The recognizable shape of a public status page: a strip of daily bars,
// worst-first color, and a handful of window percentages above it. Built
// once here so both the monitor detail page and the public status page
// render the same thing instead of two slightly different approximations.

function dayColor(d: DailyUptime): string {
  if (d.total === 0) return "var(--color-border)";
  if (d.failed === 0) return "var(--color-success)";
  if (d.failed === d.total) return "var(--color-danger)";
  return "var(--color-warning)";
}

function formatPercent(v: number | undefined): string {
  return v === undefined ? "—" : `%${v.toFixed(2)}`;
}

export function UptimeSummaryRow({ summary }: { summary: UptimeSummary }) {
  const { t } = useI18n();
  const s = t.monitors.sla;
  const cells: [string, number | undefined][] = [
    [s.day24h, summary.day24h],
    [s.day7, summary.day7],
    [s.day30, summary.day30],
    [s.day90, summary.day90],
  ];
  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
      {cells.map(([label, value]) => (
        <div key={label} className="min-w-0 rounded-lg border border-[var(--color-border)] px-3 py-2">
          <div className="truncate text-xs text-[var(--color-text-muted)]">{label}</div>
          <div className="truncate text-base font-semibold tabular-nums">{formatPercent(value)}</div>
        </div>
      ))}
    </div>
  );
}

export function UptimeHistoryBar({ history, locale }: { history: DailyUptime[]; locale: string }) {
  const { t } = useI18n();
  if (history.length === 0) return null;

  return (
    <div>
      <div className="flex h-8 w-full gap-px overflow-hidden rounded-md">
        {history.map((d) => (
          <div
            key={d.date}
            className="min-w-[2px] flex-1 rounded-[1px]"
            style={{ backgroundColor: dayColor(d) }}
            title={
              d.total === 0
                ? `${new Date(d.date).toLocaleDateString(locale)} — ${t.monitors.sla.noData}`
                : `${new Date(d.date).toLocaleDateString(locale)} — %${d.percent.toFixed(1)} (${d.total - d.failed}/${d.total})`
            }
          />
        ))}
      </div>
      <div className="mt-1.5 flex justify-between text-[11px] text-[var(--color-text-muted)]">
        <span>{new Date(history[0].date).toLocaleDateString(locale, { month: "short", day: "numeric" })}</span>
        <span>{new Date(history[history.length - 1].date).toLocaleDateString(locale, { month: "short", day: "numeric" })}</span>
      </div>
    </div>
  );
}
