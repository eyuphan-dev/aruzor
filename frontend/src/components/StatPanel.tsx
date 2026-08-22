import type { ChartSeries } from "./MetricChart";

export function StatPanel({
  series,
  color,
  threshold,
  thresholdLabel,
}: {
  series: ChartSeries;
  color: string;
  threshold?: number;
  thresholdLabel: string;
}) {
  const latest = series.values[series.values.length - 1];
  const isOverThreshold = threshold !== undefined && latest !== undefined && latest >= threshold;

  return (
    <div className="flex h-full flex-col items-center justify-center gap-1">
      <div
        className="text-4xl font-semibold tabular-nums"
        style={{ color: isOverThreshold ? "var(--color-danger)" : color }}
      >
        {latest === undefined ? "—" : formatStatValue(latest)}
      </div>
      {threshold !== undefined && (
        <div className="text-xs text-[var(--color-text-muted)]">
          {thresholdLabel}: {formatStatValue(threshold)}
        </div>
      )}
    </div>
  );
}

function formatStatValue(value: number): string {
  if (Number.isInteger(value)) return String(value);
  return value.toFixed(value < 10 ? 2 : 1);
}
