"use client";

import { useI18n } from "@/lib/i18n/context";
import type { MonitorCheck } from "@/lib/api";

// A plain SVG rather than the uPlot the dashboard uses. This chart has no
// axes, no tooltip and no zoom — it exists to show the shape of the last
// couple of hours at a glance, and pulling a charting library onto a phone
// for that would cost more than it returns.
//
// Failed checks are drawn as bars rather than skipped: a gap in a line
// reads as "no data", and the whole point is that something happened there.
export function LatencySparkline({ checks }: { checks: MonitorCheck[] }) {
  const { t } = useI18n();

  // The API returns newest first; a chart reads left to right in time.
  const ordered = [...checks].reverse();
  if (ordered.length < 2) return null;

  const width = 100;
  const height = 28;
  const max = Math.max(...ordered.map((c) => c.latencyMs), 1);
  const step = width / (ordered.length - 1);

  const points = ordered
    .map((c, i) => `${(i * step).toFixed(2)},${(height - (c.latencyMs / max) * height).toFixed(2)}`)
    .join(" ");

  return (
    <div>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        className="h-16 w-full"
        role="img"
        aria-label={t.monitors.latencyTitle}
      >
        <polyline points={points} fill="none" stroke="var(--color-primary)" strokeWidth="1" vectorEffect="non-scaling-stroke" />
        {ordered.map((c, i) =>
          c.ok ? null : (
            <rect
              key={c.id}
              x={Math.max(0, i * step - 0.6)}
              y={0}
              width={1.2}
              height={height}
              fill="var(--color-danger)"
              opacity={0.55}
            />
          ),
        )}
      </svg>
      {/* Only the two timestamps. A peak value sitting between them read as
          a third time label, and the worst figure is already stated above. */}
      <div className="mt-1 flex justify-between text-[10px] text-[var(--color-text-muted)]">
        <span>{new Date(ordered[0].checkedAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span>
        <span>
          {new Date(ordered[ordered.length - 1].checkedAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
        </span>
      </div>
    </div>
  );
}
