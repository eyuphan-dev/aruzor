import type { ChartSeries } from "./MetricChart";
import { seriesColor } from "@/lib/chartColors";
import { useI18n } from "@/lib/i18n/context";
import { formatMetricValue } from "@/lib/format";

// A percentage query is the one case where a lone series still has a
// meaningful whole to be a share of: the missing 100 - value. Queries here
// are built by multiplying a ratio by 100, so that's the marker to look
// for; anything else (byte counts, load averages, request rates) has no
// implied total and must not be forced into one.
function percentTotalFor(promQL: string, value: number): number | null {
  const looksLikePercent = /\*\s*100\b/.test(promQL) || /percent|_pct\b/i.test(promQL);
  return looksLikePercent && value >= 0 && value <= 100 ? 100 : null;
}

// Reduces every series to its latest value and shows the share each one
// holds — the same reduction Grafana's pie visualisation does.
export function PiePanel({
  series,
  extraSeries,
  color,
  promQL,
}: {
  series: ChartSeries;
  extraSeries: ChartSeries[];
  color: string;
  promQL: string;
}) {
  const { t } = useI18n();
  const all = [series, ...extraSeries];
  const slices = all
    .map((s, i) => ({
      label: s.label,
      value: s.values[s.values.length - 1] ?? 0,
      color: i === 0 ? color : seriesColor(i),
    }))
    .filter((s) => Number.isFinite(s.value) && s.value > 0);

  // One series on its own is 100% of itself, which tells the viewer
  // nothing — a CPU panel sitting at 4% used to render as a full circle
  // labelled 100%. Split it against the idle remainder when the metric is
  // a percentage, and fall back to showing the raw value when it isn't.
  const singleTotal = slices.length === 1 ? percentTotalFor(promQL, slices[0].value) : null;
  if (singleTotal !== null && singleTotal - slices[0].value > 0) {
    slices[0] = { ...slices[0], label: t.dashboard.pieUsed };
    slices.push({
      label: t.dashboard.pieFree,
      value: singleTotal - slices[0].value,
      color: "var(--color-border)",
    });
  } else if (slices.length === 1) {
    slices[0] = { ...slices[0], label: `${slices[0].label} · ${formatMetricValue(slices[0].value)}` };
  }

  const total = slices.reduce((sum, s) => sum + s.value, 0);
  if (total <= 0) {
    return <div className="flex h-full items-center justify-center text-sm text-[var(--color-text-muted)]">—</div>;
  }

  const radius = 60;
  const inner = 34;
  const cx = 70;
  const cy = 70;

  let angle = -Math.PI / 2; // start at 12 o'clock
  const paths = slices.map((s) => {
    const sweep = (s.value / total) * Math.PI * 2;
    const end = angle + sweep;
    // A full-circle arc can't be drawn with a single A command (start and
    // end points coincide), so a lone slice becomes two half arcs.
    const d =
      sweep >= Math.PI * 2 - 1e-6
        ? donutFullCircle(cx, cy, radius, inner)
        : donutSlice(cx, cy, radius, inner, angle, end);
    angle = end;
    return { ...s, d, percent: (s.value / total) * 100 };
  });

  return (
    // Stacked (svg above legend) below sm, side by side from sm up. A
    // narrow phone panel gives the pie and the legend the same ~150px
    // column when they sit side by side, which is what forced every label
    // down to "K…" / "B…" no matter how short the names were — there just
    // wasn't room. Stacking hands the legend the panel's full width
    // instead, so labels have somewhere to actually be read.
    <div className="flex h-full flex-col items-center gap-2 overflow-hidden sm:flex-row sm:gap-3">
      <svg viewBox="0 0 140 140" className="h-auto max-h-[120px] w-auto shrink-0 sm:h-full sm:max-h-[150px]">
        {paths.map((p) => (
          <path key={p.label} d={p.d} fill={p.color} stroke="var(--color-card)" strokeWidth="1" />
        ))}
      </svg>
      <div className="flex min-h-0 w-full min-w-0 flex-1 flex-col gap-0.5 overflow-y-auto text-xs">
        {paths.map((p) => (
          <div key={p.label} className="flex items-center gap-1.5">
            <span className="h-2 w-2 shrink-0 rounded-sm" style={{ background: p.color }} />
            <span className="truncate text-[var(--color-text-muted)]" title={p.label}>
              {p.label}
            </span>
            <span className="ml-auto shrink-0 tabular-nums">{p.percent.toFixed(1)}%</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function polar(cx: number, cy: number, r: number, angle: number): [number, number] {
  return [cx + r * Math.cos(angle), cy + r * Math.sin(angle)];
}

function donutSlice(cx: number, cy: number, outer: number, inner: number, start: number, end: number): string {
  const large = end - start > Math.PI ? 1 : 0;
  const [ox1, oy1] = polar(cx, cy, outer, start);
  const [ox2, oy2] = polar(cx, cy, outer, end);
  const [ix2, iy2] = polar(cx, cy, inner, end);
  const [ix1, iy1] = polar(cx, cy, inner, start);
  return [
    `M ${ox1} ${oy1}`,
    `A ${outer} ${outer} 0 ${large} 1 ${ox2} ${oy2}`,
    `L ${ix2} ${iy2}`,
    `A ${inner} ${inner} 0 ${large} 0 ${ix1} ${iy1}`,
    "Z",
  ].join(" ");
}

function donutFullCircle(cx: number, cy: number, outer: number, inner: number): string {
  return [
    `M ${cx - outer} ${cy}`,
    `A ${outer} ${outer} 0 1 1 ${cx + outer} ${cy}`,
    `A ${outer} ${outer} 0 1 1 ${cx - outer} ${cy}`,
    `M ${cx - inner} ${cy}`,
    `A ${inner} ${inner} 0 1 0 ${cx + inner} ${cy}`,
    `A ${inner} ${inner} 0 1 0 ${cx - inner} ${cy}`,
    "Z",
  ].join(" ");
}
