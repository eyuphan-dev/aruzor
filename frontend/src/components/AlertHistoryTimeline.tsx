"use client";

import { useI18n } from "@/lib/i18n/context";
import { formatDateTime } from "@/lib/format";
import type { AlertEvent } from "@/lib/api";

// A list of state changes answers "what happened" but not "how often" or
// "for how long" — and those are the questions that decide whether a
// threshold is set right. This draws the same events as a timeline of
// firing periods plus the three numbers worth reading off it.

type Period = { start: number; end: number | null };

// Pairs each "fired" with the "resolved" that follows it. Events arrive
// newest-first, so they are reversed into chronological order before
// pairing. An unmatched trailing "fired" means the rule is still firing —
// that period is left open and drawn to the right-hand edge.
function firingPeriods(events: AlertEvent[]): Period[] {
  const chronological = [...events].reverse();
  const periods: Period[] = [];
  let open: number | null = null;

  for (const ev of chronological) {
    const at = new Date(ev.createdAt).getTime();
    if (ev.event === "fired") {
      // Consecutive "fired" events without a "resolved" between them would
      // otherwise start a second period and lose the first.
      if (open === null) open = at;
    } else if (open !== null) {
      periods.push({ start: open, end: at });
      open = null;
    }
  }
  if (open !== null) periods.push({ start: open, end: null });
  return periods;
}

function formatDuration(ms: number, locale: string): string {
  const minutes = Math.round(ms / 60000);
  if (minutes < 60) return `${minutes} ${locale === "tr" ? "dk" : "min"}`;
  const hours = minutes / 60;
  if (hours < 24) return `${hours.toFixed(1)} ${locale === "tr" ? "sa" : "h"}`;
  return `${(hours / 24).toFixed(1)} ${locale === "tr" ? "gün" : "d"}`;
}

// `now` is passed in rather than read here: reading the clock during render
// is impure, and the alerts page already keeps a ticking value for its
// snooze countdowns — sharing it also keeps the two in step.
export function AlertHistoryTimeline({ events, now }: { events: AlertEvent[]; now: number }) {
  const { t, locale } = useI18n();

  const periods = firingPeriods(events);

  // The window starts at the oldest event rather than a fixed "last 7 days":
  // a rule that fired once a month ago would otherwise render an empty bar.
  const oldest = events.reduce(
    (min, ev) => Math.min(min, new Date(ev.createdAt).getTime()),
    now,
  );
  const span = Math.max(now - oldest, 60_000);

  const firedCount = events.filter((e) => e.event === "fired").length;
  const totalFiringMs = periods.reduce((sum, p) => sum + ((p.end ?? now) - p.start), 0);
  const lastFired = events.find((e) => e.event === "fired");
  const stillFiring = periods.some((p) => p.end === null);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs">
        <span className="text-[var(--color-text-muted)]">
          {t.alerts.historyFiredCount}: <span className="font-medium text-[var(--color-text)]">{firedCount}</span>
        </span>
        <span className="text-[var(--color-text-muted)]">
          {t.alerts.historyTotalTime}:{" "}
          <span className="font-medium text-[var(--color-text)]">{formatDuration(totalFiringMs, locale)}</span>
        </span>
        {lastFired && (
          <span className="text-[var(--color-text-muted)]">
            {t.alerts.historyLastFired}:{" "}
            <span className="font-medium text-[var(--color-text)]">{formatDateTime(lastFired.createdAt)}</span>
          </span>
        )}
      </div>

      <div>
        <div
          className="relative h-6 w-full overflow-hidden rounded-md bg-[var(--color-success)]/15"
          role="img"
          aria-label={t.alerts.historyTimelineLabel}
        >
          {periods.map((p) => {
            const left = ((p.start - oldest) / span) * 100;
            const width = (((p.end ?? now) - p.start) / span) * 100;
            return (
              <div
                key={p.start}
                title={`${formatDateTime(new Date(p.start))} → ${p.end ? formatDateTime(new Date(p.end)) : "…"}`}
                className="absolute inset-y-0 bg-[var(--color-danger)]"
                style={{
                  left: `${left}%`,
                  // A period shorter than the pixel grid would vanish; a rule
                  // that fired and cleared within a minute still happened.
                  width: `${Math.max(width, 0.6)}%`,
                }}
              />
            );
          })}
        </div>
        <div className="mt-1 flex justify-between text-[10px] text-[var(--color-text-muted)]">
          <span>{formatDateTime(new Date(oldest))}</span>
          <span>{stillFiring ? t.alerts.state.firing : t.alerts.historyNow}</span>
        </div>
      </div>
    </div>
  );
}
