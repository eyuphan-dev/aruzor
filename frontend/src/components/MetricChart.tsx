"use client";

import { useEffect, useMemo, useRef } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import { seriesColor } from "@/lib/chartColors";
import { formatMetricValue as formatValue } from "@/lib/format";

export type ChartSeries = {
  label: string;
  timestamps: number[];
  values: number[];
};

export type ChartAnnotation = {
  id: string;
  label: string;
  at: number; // unix seconds
};

export type ChartKind = "line" | "area" | "bar";

type Props = {
  series: ChartSeries | null;
  extraSeries?: ChartSeries[];
  color?: string;
  threshold?: number;
  annotations?: ChartAnnotation[];
  kind?: ChartKind;
  thresholdLabel?: string;
};

// uPlot's built-in time axis defaults to 12-hour (am/pm) labels; format
// ticks as 24-hour clock instead, still showing the date on the first
// tick of a new day.
function formatTimeAxis24h(_self: uPlot, splits: number[]): string[] {
  let prevDay: string | null = null;
  return splits.map((ts) => {
    const d = new Date(ts * 1000);
    const time = `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
    const day = `${d.getMonth() + 1}/${d.getDate()}/${String(d.getFullYear()).slice(-2)}`;
    if (day !== prevDay) {
      prevDay = day;
      return `${time}\n${day}`;
    }
    return time;
  });
}

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
}

// Draws a dashed vertical line + short label for each annotation that
// falls within the currently visible x-range. Reads from a ref (rather
// than closing over the prop) so it stays fresh across setData/resize
// without needing to rebuild the whole chart.
function drawAnnotations(u: uPlot, annotationsRef: { current: ChartAnnotation[] }) {
  const annotations = annotationsRef.current;
  if (annotations.length === 0) return;

  const ctx = u.ctx;
  const [xMin, xMax] = [u.scales.x.min, u.scales.x.max];
  if (xMin == null || xMax == null) return;

  ctx.save();
  ctx.strokeStyle = "#9ca3af";
  ctx.setLineDash([4, 4]);
  ctx.lineWidth = 1;
  ctx.font = "10px sans-serif";
  ctx.fillStyle = "#9ca3af";

  for (const a of annotations) {
    if (a.at < xMin || a.at > xMax) continue;
    const x = u.valToPos(a.at, "x", true);
    ctx.beginPath();
    ctx.moveTo(x, u.bbox.top);
    ctx.lineTo(x, u.bbox.top + u.bbox.height);
    ctx.stroke();
    ctx.fillText(a.label, x + 3, u.bbox.top + 10);
  }
  ctx.restore();
}

// Hover tooltip: a plain absolutely-positioned div driven by uPlot's
// cursor hook. Built as a plugin (rather than React state) so moving the
// mouse doesn't re-render the whole panel tree on every pixel.
function tooltipPlugin(): uPlot.Plugin {
  let el: HTMLDivElement | null = null;

  return {
    hooks: {
      init: (u) => {
        el = document.createElement("div");
        el.className = "aruzor-chart-tooltip";
        el.style.display = "none";
        u.over.appendChild(el);
      },
      setCursor: (u) => {
        if (!el) return;
        const { idx, left, top } = u.cursor;
        if (idx == null || left == null || left < 0) {
          el.style.display = "none";
          return;
        }
        const ts = u.data[0][idx] as number;
        const time = new Date(ts * 1000).toLocaleString("tr-TR", { hour12: false });

        const rows: string[] = [];
        for (let i = 1; i < u.series.length; i++) {
          const s = u.series[i];
          if (s.show === false) continue;
          const val = (u.data[i] as (number | null)[])[idx];
          const stroke = typeof s.stroke === "function" ? s.stroke(u, i) : s.stroke;
          const dot = typeof stroke === "string" ? stroke : "currentColor";
          const label = typeof s.label === "string" ? s.label : "";
          rows.push(
            `<div class="aruzor-tt-row"><span class="aruzor-tt-dot" style="background:${escapeHtml(dot)}"></span>` +
              `<span class="aruzor-tt-label">${escapeHtml(label)}</span>` +
              `<span class="aruzor-tt-val">${formatValue(val)}</span></div>`,
          );
        }

        el.innerHTML = `<div class="aruzor-tt-time">${escapeHtml(time)}</div>${rows.join("")}`;
        el.style.display = "block";

        // Flip to the other side of the cursor near the right/bottom edge
        // so the tooltip never gets clipped by the panel.
        const w = el.offsetWidth;
        const h = el.offsetHeight;
        const x = left + w + 16 > u.over.clientWidth ? left - w - 12 : left + 12;
        const y = Math.min(Math.max(top ?? 0, 0), Math.max(u.over.clientHeight - h - 4, 0));
        el.style.transform = `translate(${x}px, ${y}px)`;
      },
      destroy: () => {
        el?.remove();
        el = null;
      },
    },
  };
}

// Maps a series onto a reference timestamp axis. Prometheus returns the
// same step for every series in one query, so this is usually an exact
// index-for-index match; the lookup map covers ragged edges (a series
// that started late or ended early) by leaving those points as gaps.
function alignTo(xs: number[], s: ChartSeries): (number | null)[] {
  if (s.timestamps.length === xs.length && s.timestamps[0] === xs[0]) return s.values;
  const byTs = new Map<number, number>();
  for (let i = 0; i < s.timestamps.length; i++) byTs.set(s.timestamps[i], s.values[i]);
  return xs.map((x) => byTs.get(x) ?? null);
}

export function MetricChart({
  series,
  extraSeries = [],
  color = "#02c39a",
  threshold,
  annotations,
  kind = "line",
  thresholdLabel = "Eşik",
}: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const plotRef = useRef<uPlot | null>(null);
  const annotationsRef = useRef<ChartAnnotation[]>(annotations ?? []);
  const dataRef = useRef<uPlot.AlignedData | null>(null);

  useEffect(() => {
    annotationsRef.current = annotations ?? [];
    plotRef.current?.redraw();
  }, [annotations]);

  const all = useMemo(() => (series ? [series, ...extraSeries] : []), [series, extraSeries]);
  const hasThreshold = threshold !== undefined;

  // Everything that changes the *shape* of the chart, collapsed into one
  // string. The chart is rebuilt only when this changes; plain data updates
  // go through setData so zoom/cursor state and the rendered frame survive
  // each auto-refresh tick.
  const structure = series
    ? `${kind}|${all.length}|${hasThreshold}|${thresholdLabel}|${all.map((s) => s.label).join(" ")}`
    : "";

  // Every series is re-sampled onto the first series' timestamps so they
  // share one x-axis, which is what uPlot's aligned-data format needs.
  const data = useMemo<uPlot.AlignedData | null>(() => {
    if (!series) return null;
    const xs = series.timestamps;
    return [
      xs,
      ...all.map((s) => alignTo(xs, s)),
      ...(hasThreshold ? [xs.map(() => threshold as number)] : []),
    ] as uPlot.AlignedData;
  }, [series, all, hasThreshold, threshold]);

  // Declared before the creation effect so the ref is already populated by
  // the time the chart is first built. On data-only updates this is the
  // only effect that runs.
  useEffect(() => {
    dataRef.current = data;
    const plot = plotRef.current;
    // On a structure change this runs before the rebuild effect below; the
    // series-count check stops it pushing mismatched data into the
    // about-to-be-replaced instance.
    if (plot && data && plot.series.length === data.length) {
      plot.setData(data);
    }
  }, [data]);

  useEffect(() => {
    const container = containerRef.current;
    const initialData = dataRef.current;
    if (!container || !structure || !initialData) return;

    const size = () => ({ width: container.clientWidth, height: container.clientHeight });
    const barsPath = kind === "bar" ? uPlot.paths.bars?.({ size: [0.7, Infinity] }) : undefined;
    const seriesCount = all.length;

    const opts: uPlot.Options = {
      ...size(),
      series: [
        {},
        ...all.map((s, i) => {
          const stroke = i === 0 ? color : seriesColor(i);
          const seriesOpts: uPlot.Series = {
            label: s.label,
            stroke,
            width: kind === "bar" ? 1 : 2,
            points: { show: false },
          };
          // A single-series area/bar panel reads best filled; with several
          // series, overlapping fills turn into mud, so only the line
          // itself is drawn once there's more than one.
          if (kind === "area") seriesOpts.fill = seriesCount === 1 ? `${stroke}33` : `${stroke}18`;
          if (kind === "bar") {
            seriesOpts.fill = `${stroke}bb`;
            seriesOpts.paths = barsPath;
          }
          if (kind === "line" && seriesCount === 1) seriesOpts.fill = `${stroke}22`;
          return seriesOpts;
        }),
        ...(hasThreshold
          ? [
              {
                label: thresholdLabel,
                stroke: "#ef4444",
                width: 1,
                dash: [6, 4],
                points: { show: false },
              } satisfies uPlot.Series,
            ]
          : []),
      ],
      axes: [
        {
          stroke: "#1f2937",
          grid: { stroke: "#1f293726" },
          font: "500 11px sans-serif",
          values: formatTimeAxis24h,
        },
        {
          stroke: "#1f2937",
          grid: { stroke: "#1f293726" },
          font: "500 11px sans-serif",
          values: (_u, splits) => splits.map((v) => formatValue(v)),
        },
      ],
      scales: { x: { time: true } },
      legend: { show: false },
      cursor: { drag: { x: true, y: false, setScale: true }, focus: { prox: 24 } },
      plugins: [tooltipPlugin()],
      hooks: { draw: [(u) => drawAnnotations(u, annotationsRef)] },
    };

    const plot = new uPlot(opts, initialData, container);
    plotRef.current = plot;

    // The panel's flex layout can still be settling when the chart is
    // built, and uPlot only ranges its scales from data — never from a
    // later setSize(). Re-pushing the data once the box has real
    // dimensions is what makes the series actually appear; without it the
    // x scale stays null and nothing is plotted.
    const applySize = () => {
      const s = size();
      if (s.width === 0 || s.height === 0) return;
      plot.setSize(s);
      if (plot.scales.x.min == null && dataRef.current) plot.setData(dataRef.current);
    };
    applySize();

    const resizeObserver = new ResizeObserver(applySize);
    resizeObserver.observe(container);

    return () => {
      resizeObserver.disconnect();
      plot.destroy();
      if (plotRef.current === plot) plotRef.current = null;
    };
    // `all` tracks `structure` (same inputs) and `color` is applied by the
    // effect below, so neither belongs here — including them would tear the
    // chart down on every refresh.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [structure]);

  // Restyle in place when the color prop changes, instead of rebuilding.
  // uPlot normalizes series.stroke/fill into functions of (self, seriesIdx)
  // at construction time; assigning plain strings here bypasses that and
  // crashes the next draw() call ("s.stroke is not a function"), so these
  // must be reassigned as functions too.
  useEffect(() => {
    const plot = plotRef.current;
    if (!plot || !plot.series[1]) return;
    plot.series[1].stroke = () => color;
    if (plot.series[1].fill) plot.series[1].fill = () => `${color}22`;
    plot.redraw();
  }, [color]);

  if (!series) {
    return <div className="flex h-full items-center justify-center text-sm text-[var(--color-text-muted)]">…</div>;
  }

  return <div ref={containerRef} className="h-full w-full" />;
}
