import type { ChartSeries } from "./MetricChart";

// Assumes the metric is a 0-100 percentage (the common case for CPU/memory/
// disk usage panels) — arbitrary-range gauges would need a configurable
// min/max, which isn't worth the extra UI for this dashboard's use cases.
export function GaugePanel({ series, color, threshold }: { series: ChartSeries; color: string; threshold?: number }) {
  const latest = series.values[series.values.length - 1];
  const percent = latest === undefined ? 0 : Math.max(0, Math.min(100, latest));
  const isOverThreshold = threshold !== undefined && latest !== undefined && latest >= threshold;

  const radius = 70;
  const circumference = Math.PI * radius; // half circle
  const offset = circumference * (1 - percent / 100);

  return (
    <div className="flex h-full flex-col items-center justify-center">
      <svg viewBox="0 0 180 100" className="w-full max-w-[220px]">
        <path
          d="M 20 90 A 70 70 0 0 1 160 90"
          fill="none"
          stroke="var(--color-border)"
          strokeWidth="14"
          strokeLinecap="round"
        />
        <path
          d="M 20 90 A 70 70 0 0 1 160 90"
          fill="none"
          stroke={isOverThreshold ? "var(--color-danger)" : color}
          strokeWidth="14"
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={offset}
        />
        <text x="90" y="80" textAnchor="middle" className="fill-[var(--color-text)] text-2xl font-semibold">
          {latest === undefined ? "—" : `${Math.round(percent)}%`}
        </text>
      </svg>
    </div>
  );
}
