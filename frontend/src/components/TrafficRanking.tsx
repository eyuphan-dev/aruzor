"use client";

import type { TrafficDim } from "@/lib/api";
import { formatBytes, formatCount } from "@/lib/format";
import { useI18n } from "@/lib/i18n/context";

type Metric = "requests" | "bytes" | "errors";

// A ranked list with the bar drawn behind the row rather than beside it.
//
// A pie would be the obvious choice for "top IPs", but these lists are
// long-tailed: the leader usually holds a few percent, and a dozen slices
// of four percent each are indistinguishable. A bar whose length is
// relative to the leader answers the actual question — how far ahead is
// first place — at a glance, and keeps the exact figure readable next to it.
export function TrafficRanking({
  rows,
  metric = "requests",
  secondary,
  emptyLabel,
  note,
  keyLabel,
}: {
  rows: TrafficDim[];
  metric?: Metric;
  // A second figure shown after the primary one: bandwidth next to a
  // request count is what turns "this IP is noisy" into "this IP is
  // expensive", and the two rarely point at the same row.
  secondary?: Metric;
  emptyLabel: string;
  note?: string;
  keyLabel?: string;
}) {
  const { t } = useI18n();

  if (rows.length === 0) {
    return <p className="py-6 text-center text-sm text-[var(--color-text-muted)]">{emptyLabel}</p>;
  }

  const max = Math.max(...rows.map((r) => valueOf(r, metric)), 1);

  return (
    <div className="flex flex-col gap-1.5">
      {keyLabel && <p className="text-[11px] uppercase tracking-wide text-[var(--color-text-muted)]">{keyLabel}</p>}
      {rows.map((row) => {
        const value = valueOf(row, metric);
        const share = Math.max(2, (value / max) * 100);
        return (
          <div key={row.key} className="relative overflow-hidden rounded-md">
            <div
              aria-hidden="true"
              className="absolute inset-y-0 left-0 bg-[var(--color-primary)]/15"
              style={{ width: `${share}%` }}
            />
            <div className="relative flex items-center gap-3 px-2 py-1.5 text-sm">
              <span className="min-w-0 flex-1 truncate font-mono text-[13px]" title={row.key}>
                {row.key}
              </span>
              {secondary && (
                <span className="shrink-0 text-xs tabular-nums text-[var(--color-text-muted)]">
                  {format(row, secondary)}
                </span>
              )}
              <span className="shrink-0 tabular-nums">{format(row, metric)}</span>
            </div>
          </div>
        );
      })}
      {note && <p className="mt-1 text-[11px] leading-snug text-[var(--color-text-muted)]">{note}</p>}
      {rows.length > 0 && !note && <span className="sr-only">{t.traffic.notes.ranking}</span>}
    </div>
  );
}

function valueOf(row: TrafficDim, metric: Metric): number {
  return metric === "bytes" ? row.bytes : metric === "errors" ? row.errors : row.requests;
}

function format(row: TrafficDim, metric: Metric): string {
  return metric === "bytes" ? formatBytes(row.bytes) : formatCount(valueOf(row, metric));
}
