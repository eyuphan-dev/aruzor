"use client";

import { useI18n } from "@/lib/i18n/context";
import type { MonitorTimelineBucket } from "@/lib/api";

// The bar that answers "when was it bad". Telegram deliberately stays quiet
// through short stalls now, so this is where those stalls have to be
// visible — otherwise the quiet would be indistinguishable from a day with
// nothing wrong, which is the failure mode of every alerting system that
// only ever gets less noisy.
//
// A step is amber when some checks in it failed and red when they all did:
// the difference between "it wobbled" and "it was gone" is exactly what the
// notification rule now hinges on, so the picture should show it too.
type State = "ok" | "wobbled" | "partial" | "down" | "empty";

// Four states, because the notification rule now turns on exactly these
// distinctions and the picture should show what the rule sees:
//   wobbled - every check passed, but some needed a retry
//   partial - some checks failed outright
//   down    - nothing got through in this step
function stateOf(b: MonitorTimelineBucket): State {
  if (b.total === 0) return "empty";
  if (b.failed === 0) return b.retried > 0 ? "wobbled" : "ok";
  return b.failed === b.total ? "down" : "partial";
}

const COLORS: Record<State, string> = {
  ok: "var(--color-success)",
  // A tint rather than a fifth hue: this is a weaker form of the amber
  // beside it, and reading it as such is the point.
  wobbled: "color-mix(in srgb, var(--color-warning) 45%, var(--color-success))",
  partial: "var(--color-warning)",
  down: "var(--color-danger)",
  empty: "var(--color-border)",
};

function hour(iso: string) {
  return new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

export function HealthTimeline({ buckets }: { buckets: MonitorTimelineBucket[] }) {
  const { t } = useI18n();
  if (buckets.length === 0) return null;

  const withData = buckets.filter((b) => b.total > 0);
  const bad = buckets.filter((b) => b.failed > 0);
  const wobbly = buckets.filter((b) => b.failed === 0 && b.retried > 0);

  return (
    <div>
      {/* A 48-step day on a 320px screen leaves under 5px per bar, so a 2px
          gap was taking almost half the width and the colour became hard to
          read at a glance. The gap only has to separate, not decorate. */}
      <div className="flex h-9 w-full items-stretch gap-px">
        {buckets.map((b) => {
          const state = stateOf(b);
          const label =
            b.total === 0
              ? `${hour(b.start)} — ${t.monitors.timelineNoData}`
              : `${hour(b.start)} — ${b.total - b.failed}/${b.total} ${t.monitors.timelineOkOf}` +
                (b.retried > 0 ? `, ${b.retried} ${t.monitors.timelineRetriedOf}` : "") +
                `, ${b.medianMs} ms`;
          return (
            <div
              key={b.start}
              title={label}
              className="min-w-0 flex-1 rounded-[1px]"
              style={{ backgroundColor: COLORS[state], opacity: state === "empty" ? 0.5 : 1 }}
            />
          );
        })}
      </div>

      <div className="mt-1 flex justify-between text-[10px] text-[var(--color-text-muted)]">
        <span>{t.monitors.timeline24h}</span>
        <span>{t.monitors.timelineNow}</span>
      </div>

      {/* Naming the bad stretches in words, because a colour on a 2px bar is
          hard to read on a phone and impossible to read aloud to someone. */}
      <div className="mt-3 flex flex-col gap-1.5">
        {bad.length > 0 && (
          <p className="text-xs leading-relaxed text-[var(--color-text-muted)]">
            {t.monitors.timelineTrouble}{" "}
            <span className="text-[var(--color-text)]">{bad.slice(-6).map((b) => hour(b.start)).join(" · ")}</span>
          </p>
        )}
        {wobbly.length > 0 && (
          <p className="text-xs leading-relaxed text-[var(--color-text-muted)]">
            {t.monitors.timelineWobbly}{" "}
            <span className="text-[var(--color-text)]">{wobbly.slice(-6).map((b) => hour(b.start)).join(" · ")}</span>
          </p>
        )}
        {bad.length === 0 && wobbly.length === 0 && withData.length > 0 && (
          <p className="text-xs leading-relaxed text-[var(--color-text-muted)]">{t.monitors.timelineClean}</p>
        )}
      </div>
    </div>
  );
}
