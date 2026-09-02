"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useI18n } from "@/lib/i18n/context";
import { useAuth } from "@/lib/auth/context";
import { getTraffic, type TrafficOverview } from "@/lib/api";
import { formatBytesPerSecond, formatCount, formatRate } from "@/lib/format";

// A one-line summary of the Traffic page, sat above the dashboard grid.
//
// The full page is fourteen panels; putting those on the dashboard would
// bury the panels someone actually chose to put there. But "how much
// traffic is this box taking right now" is a question you ask on the way
// past, not one worth navigating for — so the four headline numbers and the
// shape of the last day live here, and everything else stays one click
// away.
//
// It follows the discovery panel's precedent: a fixed strip above the grid
// rather than a draggable panel, because the dashboard grid runs on PromQL
// and none of these numbers come from Prometheus.
export function TrafficStrip() {
  const { t } = useI18n();
  const { session } = useAuth();
  const [data, setData] = useState<TrafficOverview | null>(null);

  // The endpoint is admin+, so for anyone below that this component must
  // not even ask — a viewer would get a 403 on every dashboard load, and
  // visitors' IP addresses are not theirs to see.
  const allowed = session?.role === "admin" || session?.role === "super_admin";

  useEffect(() => {
    if (!allowed) return;
    let cancelled = false;
    getTraffic("24h")
      .then((res) => {
        if (!cancelled) setData(res);
      })
      .catch(() => {
        // A dashboard is not the place to report that an optional summary
        // could not load; the Traffic page itself explains any problem.
      });
    return () => {
      cancelled = true;
    };
  }, [allowed]);

  // Nothing ingested means the feature is not set up on this server. The
  // dashboard is the wrong place to teach that — the Traffic page carries
  // the setup instructions — so the strip simply stays out of the way.
  if (!allowed || !data || !data.hasData) return null;

  const cells: { label: string; value: string; tone?: "warn" | "danger" }[] = [
    { label: t.traffic.panels.requestRate, value: `${formatRate(data.totals.requestsPerSecond)}/s` },
    { label: t.traffic.panels.egress, value: formatBytesPerSecond(data.totals.bytesPerSecond) },
    {
      label: t.traffic.stats.errorRate,
      value: `%${data.totals.errorRate.toFixed(2)}`,
      tone: data.totals.errors5xx > 0 ? "danger" : undefined,
    },
    {
      label: t.traffic.stats.unauthorized,
      value: formatCount(data.totals.unauthorized),
      tone: data.totals.unauthorized > 0 ? "warn" : undefined,
    },
  ];

  return (
    <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-baseline gap-2">
          <h2 className="text-sm font-medium">{t.traffic.title}</h2>
          <span className="text-xs text-[var(--color-text-muted)]">{t.traffic.ranges["24h"]}</span>
        </div>
        <Link href="/traffic" className="text-xs font-medium text-[var(--color-primary)] hover:underline">
          {t.traffic.seeAll} →
        </Link>
      </div>

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <div className="grid flex-1 grid-cols-2 gap-3 lg:grid-cols-4">
          {cells.map((c) => (
            <div key={c.label}>
              <p className="truncate text-[11px] text-[var(--color-text-muted)]">{c.label}</p>
              <p
                className="text-lg font-semibold tabular-nums"
                style={{
                  color:
                    c.tone === "danger"
                      ? "var(--color-danger)"
                      : c.tone === "warn"
                        ? "var(--color-warning)"
                        : "var(--color-text)",
                }}
              >
                {c.value}
              </p>
            </div>
          ))}
        </div>
        <Sparkline points={data.series.map((p) => p.requests)} />
      </div>
    </div>
  );
}

// Deliberately not the uPlot the dashboard uses: this has no axes, no
// tooltip and no zoom. It is here to show whether the day was flat or
// spiky, and pulling a charting library onto a phone for that costs more
// than it returns.
function Sparkline({ points }: { points: number[] }) {
  const { t } = useI18n();
  if (points.length < 2) return null;

  const width = 200;
  const height = 36;
  const max = Math.max(...points, 1);
  const step = width / (points.length - 1);
  const line = points.map((v, i) => `${(i * step).toFixed(1)},${(height - (v / max) * height).toFixed(1)}`).join(" ");

  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      className="h-10 w-full shrink-0 sm:w-48"
      role="img"
      aria-label={t.traffic.panels.requestRate}
    >
      <polygon points={`0,${height} ${line} ${width},${height}`} fill="var(--color-primary)" opacity="0.12" />
      <polyline
        points={line}
        fill="none"
        stroke="var(--color-primary)"
        strokeWidth="1.2"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}
