import type { ChartSeries } from "./MetricChart";
import { seriesColor } from "@/lib/chartColors";

export type LegendLabels = {
  last: string;
  min: string;
  max: string;
  avg: string;
};

function stats(values: number[]) {
  const finite = values.filter((v) => Number.isFinite(v));
  if (finite.length === 0) return null;
  let min = finite[0];
  let max = finite[0];
  let sum = 0;
  for (const v of finite) {
    if (v < min) min = v;
    if (v > max) max = v;
    sum += v;
  }
  return { last: finite[finite.length - 1], min, max, avg: sum / finite.length };
}

function fmt(v: number): string {
  const abs = Math.abs(v);
  if (abs >= 1e9) return `${(v / 1e9).toFixed(2)}B`;
  if (abs >= 1e6) return `${(v / 1e6).toFixed(2)}M`;
  if (abs >= 1e3) return `${(v / 1e3).toFixed(2)}K`;
  if (Number.isInteger(v)) return String(v);
  return v.toFixed(abs < 10 ? 2 : 1);
}

// Summary row under a chart: the numbers people actually read off a panel
// without hovering — current value plus the min/max/average of the window.
export function ChartLegend({
  series,
  extraSeries,
  color,
  labels,
}: {
  series: ChartSeries;
  extraSeries: ChartSeries[];
  color: string;
  labels: LegendLabels;
}) {
  const all = [series, ...extraSeries];
  const rows = all
    .map((s, i) => ({ label: s.label, color: i === 0 ? color : seriesColor(i), s: stats(s.values) }))
    .filter((r) => r.s !== null);

  if (rows.length === 0) return null;

  // With one series the four numbers fit on a single line; with several,
  // each gets its own compact row so labels stay readable.
  if (rows.length === 1) {
    const { s } = rows[0];
    return (
      <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-0.5 px-1 pt-1 text-[10px] text-[var(--color-text-muted)]">
        <span>
          {labels.last} <span className="font-medium tabular-nums text-[var(--color-text)]">{fmt(s!.last)}</span>
        </span>
        <span>
          {labels.min} <span className="tabular-nums">{fmt(s!.min)}</span>
        </span>
        <span>
          {labels.max} <span className="tabular-nums">{fmt(s!.max)}</span>
        </span>
        <span>
          {labels.avg} <span className="tabular-nums">{fmt(s!.avg)}</span>
        </span>
      </div>
    );
  }

  return (
    <div className="max-h-[74px] shrink-0 overflow-y-auto px-1 pt-1 text-[10px]">
      {rows.map((r) => (
        <div key={r.label} className="flex items-center gap-1.5">
          <span className="h-2 w-2 shrink-0 rounded-sm" style={{ background: r.color }} />
          <span className="truncate text-[var(--color-text-muted)]" title={r.label}>
            {r.label}
          </span>
          <span className="ml-auto shrink-0 font-medium tabular-nums">{fmt(r.s!.last)}</span>
          <span className="w-12 shrink-0 text-right tabular-nums text-[var(--color-text-muted)]">{fmt(r.s!.max)}</span>
        </div>
      ))}
    </div>
  );
}
