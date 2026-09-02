"use client";

import { useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n/context";
import { useMetricSeries } from "@/lib/useMetricSeries";
import { MetricChart, type ChartAnnotation } from "./MetricChart";
import { ChartLegend } from "./ChartLegend";
import { StatPanel } from "./StatPanel";
import { GaugePanel } from "./GaugePanel";
import { PiePanel } from "./PiePanel";
import type { PanelType } from "@/lib/api";
import { panelSource } from "@/lib/panelSource";

// Chart kinds a user can flip between straight from the panel header —
// the same set Grafana exposes in its visualisation picker, trimmed to the
// ones that make sense for Prometheus time series.
const PANEL_TYPES: PanelType[] = ["line", "area", "bar", "pie", "stat", "gauge"];

function TypeIcon({ type }: { type: PanelType }) {
  const common = { fill: "none", stroke: "currentColor", strokeWidth: 1.6, strokeLinecap: "round" as const, strokeLinejoin: "round" as const };
  switch (type) {
    case "line":
      return (
        <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" {...common}>
          <path d="M2 12 L6 7 L9.5 9.5 L14 4" />
        </svg>
      );
    case "area":
      return (
        <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" {...common}>
          <path d="M2 12 L6 7 L9.5 9.5 L14 4 L14 13 L2 13 Z" fill="currentColor" fillOpacity="0.25" />
        </svg>
      );
    case "bar":
      return (
        <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" {...common}>
          <path d="M3 13 L3 8 M7 13 L7 4 M11 13 L11 9 M14.5 13 L14.5 6" strokeWidth="2" />
        </svg>
      );
    case "pie":
      return (
        <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" {...common}>
          <circle cx="8" cy="8" r="5.5" />
          <path d="M8 8 L8 2.5 M8 8 L12.9 10.6" />
        </svg>
      );
    case "stat":
      return (
        <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" {...common}>
          <path d="M4 5 L6.5 3.5 L6.5 12.5 M4.5 12.5 L9 12.5" />
        </svg>
      );
    case "gauge":
      return (
        <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" {...common}>
          <path d="M2.5 11.5 A 5.5 5.5 0 0 1 13.5 11.5" />
          <path d="M8 11.5 L11 7.5" />
        </svg>
      );
  }
}

function IconButton({
  onClick,
  title,
  children,
  active,
}: {
  onClick: () => void;
  title: string;
  children: React.ReactNode;
  active?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      title={title}
      aria-label={title}
      aria-pressed={active}
      className={`no-drag flex h-9 w-9 items-center justify-center rounded-md border transition-colors sm:h-6 sm:w-6 ${
        active
          ? "border-[var(--color-primary)] bg-[var(--color-primary)]/10 text-[var(--color-primary)]"
          : "border-transparent text-[var(--color-text-muted)] hover:border-[var(--color-border)] hover:text-[var(--color-text)]"
      }`}
    >
      {children}
    </button>
  );
}

export function DashboardPanel({
  title,
  promQL,
  color,
  panelType = "line",
  threshold,
  annotations,
  rangeSeconds,
  refreshMs,
  refreshNonce,
  editable,
  duplicate,
  shareToken,
  instanceFilter,
  onRemove,
  onDuplicate,
  onChangeType,
}: {
  title: string;
  promQL: string;
  color: string;
  panelType?: PanelType;
  threshold?: number;
  annotations?: ChartAnnotation[];
  rangeSeconds: number;
  refreshMs: number;
  refreshNonce: number;
  editable: boolean;
  // True when another panel already draws this exact query in this exact
  // visualisation. Marked rather than hidden: it is the operator's
  // dashboard, and silently dropping one of their panels would be a worse
  // surprise than telling them the two are the same.
  duplicate?: boolean;
  // Present on a shared, read-only dashboard: queries go through the share
  // endpoint instead of the authenticated one.
  shareToken?: string;
  // Restricts the drawn series to one server. Panels whose query aggregates
  // the instance label away carry no instance and are shown regardless.
  instanceFilter?: string;
  onRemove: () => void;
  onDuplicate: () => void;
  onChangeType: (type: PanelType) => void;
}) {
  const { t, locale } = useI18n();
  const { series, extraSeries, loading, error, hostFilterApplied } = useMetricSeries(
    title,
    promQL,
    rangeSeconds,
    undefined,
    refreshMs,
    refreshNonce,
    shareToken,
    instanceFilter,
  );
  const [fullscreen, setFullscreen] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);

  // Escape closes fullscreen, matching how every other overlay in the app
  // (and in Grafana) behaves.
  useEffect(() => {
    if (!fullscreen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setFullscreen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [fullscreen]);

  const exportCsv = () => {
    if (!series) return;
    const all = [series, ...extraSeries];
    const header = ["timestamp", "time", ...all.map((s) => s.label)].join(",");
    const lines = series.timestamps.map((ts, i) => {
      const time = new Date(ts * 1000).toLocaleString("tr-TR", { hour12: false });
      return [ts, `"${time}"`, ...all.map((s) => s.values[i] ?? "")].join(",");
    });
    const blob = new Blob([[header, ...lines].join("\n")], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${title.replace(/[^\w\-]+/g, "_")}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const body = (
    <>
      {loading && !series ? (
        <div className="flex h-full items-center justify-center text-sm text-[var(--color-text-muted)]">
          {t.dashboard.loading}
        </div>
      ) : error || !series ? (
        <div className="flex h-full items-center justify-center text-center text-sm text-[var(--color-text-muted)]">
          {t.dashboard.noData}
        </div>
      ) : panelType === "stat" ? (
        <StatPanel series={series} color={color} threshold={threshold} thresholdLabel={t.dashboard.thresholdLabel} />
      ) : panelType === "gauge" ? (
        <GaugePanel series={series} color={color} threshold={threshold} />
      ) : panelType === "pie" ? (
        <PiePanel series={series} extraSeries={extraSeries} color={color} promQL={promQL} />
      ) : (
        <div className="flex h-full min-h-0 flex-col">
          <div className="min-h-0 flex-1">
            <MetricChart
              series={series}
              extraSeries={extraSeries}
              color={color}
              threshold={threshold}
              annotations={annotations}
              kind={panelType === "area" ? "area" : panelType === "bar" ? "bar" : "line"}
              thresholdLabel={t.dashboard.thresholdLabel}
            />
          </div>
          <ChartLegend
            series={series}
            extraSeries={extraSeries}
            color={color}
            labels={{
              last: t.dashboard.legendLast,
              min: t.dashboard.legendMin,
              max: t.dashboard.legendMax,
              avg: t.dashboard.legendAvg,
            }}
          />
        </div>
      )}
    </>
  );

  // Which exporter this panel's numbers come from. Two panels titled
  // "İşlemci Kullanımı" are only distinguishable once the page says one is
  // reading the server and the other a container, and the badge carries the
  // query itself as its tooltip for the cases where even that is not enough.
  const source = panelSource(promQL);

  const header = (
    // flex-wrap rather than a fixed single row: the title, the source/
    // duplicate badges and the action buttons (especially the chart-type
    // picker, which expands into six icon buttons) do not all fit on one
    // line at phone widths. Without wrapping, react-grid-layout's own
    // overflow:hidden on each panel just clips whatever doesn't fit —
    // which on a phone silently hid the pie/bar/stat buttons behind the
    // panel's edge, not merely squeezed the title. Wrapping trades a
    // taller header for every control staying reachable; sm: and up keeps
    // the original single-row layout untouched.
    <div className="mb-2 flex flex-wrap items-center gap-1">
      <h3
        className="min-w-0 flex-1 basis-full truncate text-sm font-medium text-[var(--color-text)] sm:basis-auto"
        title={title}
      >
        {title}
      </h3>
      {duplicate && (
        <span
          title={t.dashboard.duplicatePanelHint}
          className="shrink-0 rounded-full border border-[var(--color-warning)] px-1.5 py-0.5 text-[10px] font-medium leading-none text-[var(--color-warning)]"
        >
          {t.dashboard.duplicatePanelBadge}
        </span>
      )}
      {source && (
        <span
          title={promQL}
          className="shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-medium leading-none"
          style={{ backgroundColor: `${source.color}22`, color: source.color }}
        >
          {source.label[locale]}
        </span>
      )}
      <span
        className="ml-1 h-2 w-2 shrink-0 rounded-full"
        style={{ backgroundColor: error ? "var(--color-danger)" : "var(--color-success)" }}
      />
      {instanceFilter && !hostFilterApplied && (
        <span
          title={t.dashboard.hostNotFilteredHint}
          className="shrink-0 rounded-full border border-[var(--color-border)] px-1.5 text-[10px] text-[var(--color-text-muted)]"
        >
          {t.dashboard.hostNotFiltered}
        </span>
      )}

      <div className="ml-auto flex flex-wrap items-center justify-end gap-0.5">
        {/* Chart-type switcher: expands into the full row on click so the
            header stays uncluttered at small panel widths. */}
        {pickerOpen ? (
          // The six type buttons are one flex child of the wrapping header
          // row above, so wrapping only there was not enough — this box
          // itself was wider than the panel and got clipped by the grid
          // item's own overflow:hidden, hiding the pie/bar/stat buttons off
          // a phone's edge instead of just crowding the header. It needs to
          // wrap onto its own second row internally, and never claim more
          // width than the panel actually has.
          <div className="no-drag flex max-w-full flex-wrap items-center gap-0.5 rounded-md border border-[var(--color-border)] p-0.5">
            {PANEL_TYPES.map((type) => (
              <IconButton
                key={type}
                title={t.dashboard.panelTypeNames[type]}
                active={panelType === type}
                onClick={() => {
                  onChangeType(type);
                  setPickerOpen(false);
                }}
              >
                <TypeIcon type={type} />
              </IconButton>
            ))}
          </div>
        ) : (
          <IconButton title={t.dashboard.changePanelType} onClick={() => setPickerOpen(true)}>
            <TypeIcon type={panelType} />
          </IconButton>
        )}

        <IconButton title={t.dashboard.exportCsv} onClick={exportCsv}>
          <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
            <path d="M8 2 L8 10 M5 7.5 L8 10.5 L11 7.5 M3 13 L13 13" />
          </svg>
        </IconButton>

        <IconButton title={fullscreen ? t.dashboard.exitFullscreen : t.dashboard.fullscreen} onClick={() => setFullscreen((v) => !v)}>
          <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
            {fullscreen ? <path d="M6 2 L6 6 L2 6 M10 14 L10 10 L14 10" /> : <path d="M2 6 L2 2 L6 2 M10 14 L14 14 L14 10" />}
          </svg>
        </IconButton>

        {editable && (
          <>
            <IconButton title={t.dashboard.duplicatePanel} onClick={onDuplicate}>
              <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
                <rect x="5.5" y="5.5" width="8" height="8" rx="1.5" />
                <path d="M10.5 3 L3.5 3 A0.5 0.5 0 0 0 3 3.5 L3 10.5" />
              </svg>
            </IconButton>
            <button
              onClick={onRemove}
              title={t.dashboard.removePanel}
              aria-label={t.dashboard.removePanel}
              className="no-drag flex h-6 w-6 items-center justify-center rounded-md border border-transparent text-[var(--color-danger)] hover:border-[var(--color-border)] hover:bg-[var(--color-danger)]/10"
            >
              <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
                <path d="M4 4 L12 12 M12 4 L4 12" />
              </svg>
            </button>
          </>
        )}
      </div>
    </div>
  );

  if (fullscreen) {
    return (
      <>
        {/* Placeholder keeps the grid cell occupied while the real panel is
            lifted into the overlay, so the layout doesn't reflow. */}
        <div className="h-full rounded-xl border border-dashed border-[var(--color-border)]" />
        <div className="fixed inset-0 z-50 flex flex-col bg-[var(--color-bg)]/95 p-4 backdrop-blur-sm sm:p-8">
          <div className="flex h-full flex-col rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
            {header}
            <div className="min-h-0 flex-1">{body}</div>
          </div>
        </div>
      </>
    );
  }

  return (
    <div className="flex h-full flex-col rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      {header}
      <div className="min-h-0 flex-1">{body}</div>
    </div>
  );
}
