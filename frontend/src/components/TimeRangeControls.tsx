"use client";

import { useI18n } from "@/lib/i18n/context";

// Relative windows only (always ending "now") — that's what an operations
// dashboard is looked at through, and it keeps auto-refresh meaningful.
export const TIME_RANGES = [
  { seconds: 5 * 60, key: "m5" },
  { seconds: 15 * 60, key: "m15" },
  { seconds: 60 * 60, key: "h1" },
  { seconds: 6 * 3600, key: "h6" },
  { seconds: 12 * 3600, key: "h12" },
  { seconds: 24 * 3600, key: "d1" },
  { seconds: 7 * 24 * 3600, key: "d7" },
  { seconds: 30 * 24 * 3600, key: "d30" },
] as const;

export const REFRESH_INTERVALS = [
  { ms: 0, key: "off" },
  { ms: 10_000, key: "s10" },
  { ms: 30_000, key: "s30" },
  { ms: 60_000, key: "m1" },
  { ms: 300_000, key: "m5" },
] as const;

export function TimeRangeControls({
  rangeSeconds,
  onRangeChange,
  refreshMs,
  onRefreshMsChange,
  onRefreshNow,
}: {
  rangeSeconds: number;
  onRangeChange: (seconds: number) => void;
  refreshMs: number;
  onRefreshMsChange: (ms: number) => void;
  onRefreshNow: () => void;
}) {
  const { t } = useI18n();

  return (
    // Wraps on its own. The parent row wraps, but this group did not, so it
    // stayed one unbreakable unit: below about 380px the two selects and the
    // refresh button together were wider than the screen and the button
    // ended up outside it.
    <div className="flex flex-wrap items-center gap-1.5">
      <select
        value={rangeSeconds}
        onChange={(e) => onRangeChange(Number(e.target.value))}
        title={t.dashboard.timeRange}
        aria-label={t.dashboard.timeRange}
        className="aruzor-select aruzor-select-auto"
      >
        {TIME_RANGES.map((r) => (
          <option key={r.key} value={r.seconds}>
            {t.dashboard.timeRanges[r.key]}
          </option>
        ))}
      </select>

      <select
        value={refreshMs}
        onChange={(e) => onRefreshMsChange(Number(e.target.value))}
        title={t.dashboard.refreshInterval}
        aria-label={t.dashboard.refreshInterval}
        className="aruzor-select aruzor-select-auto"
      >
        {REFRESH_INTERVALS.map((r) => (
          <option key={r.key} value={r.ms}>
            {t.dashboard.refreshIntervals[r.key]}
          </option>
        ))}
      </select>

      <button
        onClick={onRefreshNow}
        title={t.dashboard.refreshNow}
        aria-label={t.dashboard.refreshNow}
        className="flex min-h-9 min-w-9 items-center justify-center rounded-md border border-[var(--color-border)] px-2 text-[var(--color-text-muted)] hover:text-[var(--color-text)]"
      >
        <svg viewBox="0 0 16 16" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
          <path d="M13.5 8 A5.5 5.5 0 1 1 11.6 3.8 M13.5 1.5 L13.5 4.5 L10.5 4.5" />
        </svg>
      </button>
    </div>
  );
}
