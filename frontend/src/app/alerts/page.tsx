"use client";

import { Fragment, useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n/context";
import {
  listAlertRules,
  createAlertRule,
  setAlertRuleEnabled,
  deleteAlertRule,
  snoozeAlertRule,
  ackAlertRule,
  getAlertHistory,
  type AlertRule,
  type AlertOperator,
  type AlertEvent,
} from "@/lib/api";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { AlertHistoryTimeline } from "@/components/AlertHistoryTimeline";
import { HelpTooltip } from "@/components/HelpTooltip";
import { useAuth } from "@/lib/auth/context";
import { hasMinRole } from "@/lib/auth/roles";
import { formatDateTime } from "@/lib/format";

const OPERATORS: AlertOperator[] = [">", ">=", "<", "<=", "=="];
const SNOOZE_OPTIONS = [
  { minutes: 30, key: "snooze30m" as const },
  { minutes: 60, key: "snooze1h" as const },
  { minutes: 240, key: "snooze4h" as const },
  { minutes: 1440, key: "snooze1d" as const },
];

export default function AlertsPage() {
  const { t } = useI18n();
  const { session } = useAuth();
  const canEdit = hasMinRole(session?.role, "editor");
  const [rules, setRules] = useState<AlertRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<AlertRule | null>(null);
  const [historyOpenFor, setHistoryOpenFor] = useState<string | null>(null);
  const [historyEvents, setHistoryEvents] = useState<AlertEvent[]>([]);
  const [historyLoading, setHistoryLoading] = useState(false);

  const [name, setName] = useState("");
  const [promql, setPromql] = useState("");
  const [operator, setOperator] = useState<AlertOperator>(">");
  const [threshold, setThreshold] = useState("90");
  const [submitting, setSubmitting] = useState(false);

  // Read once per render via state (not inline Date.now() in JSX) so the
  // "snoozed" badge is derived from a value React considers stable, and
  // ticks forward on its own so the badge clears itself once a snooze
  // expires without needing a full page refresh.
  const [now, setNow] = useState<number | null>(null);
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setNow(Date.now());
    const id = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(id);
  }, []);

  const refresh = () => {
    setLoading(true);
    listAlertRules()
      .then(setRules)
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false));
  };

  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(refresh, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      await createAlertRule({ name, promql, operator, threshold: Number(threshold) });
      setName("");
      setPromql("");
      setThreshold("90");
      refresh();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSubmitting(false);
    }
  };

  const toggle = async (rule: AlertRule) => {
    try {
      await setAlertRuleEnabled(rule.id, !rule.enabled);
      refresh();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const snooze = async (rule: AlertRule, minutes: number) => {
    try {
      await snoozeAlertRule(rule.id, minutes);
      refresh();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const ack = async (rule: AlertRule) => {
    try {
      await ackAlertRule(rule.id);
      refresh();
    } catch (err) {
      setError((err as Error).message);
    }
  };

  const toggleHistory = (rule: AlertRule) => {
    if (historyOpenFor === rule.id) {
      setHistoryOpenFor(null);
      return;
    }
    setHistoryOpenFor(rule.id);
    setHistoryLoading(true);
    getAlertHistory(rule.id)
      .then(setHistoryEvents)
      .catch((err: Error) => setError(err.message))
      .finally(() => setHistoryLoading(false));
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    try {
      await deleteAlertRule(pendingDelete.id);
      refresh();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setPendingDelete(null);
    }
  };

  const isSnoozed = (rule: AlertRule) => now !== null && rule.snoozedUntil && new Date(rule.snoozedUntil).getTime() > now;

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="aruzor-page-title flex items-center gap-2 text-xl font-semibold">
          {t.alerts.title}
          <HelpTooltip text={t.alerts.helpText} />
        </h1>
        <p className="text-sm text-[var(--color-text-muted)]">{t.alerts.subtitle}</p>
      </div>

      <p className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] px-4 py-3 text-xs text-[var(--color-text-muted)]">
        {t.alerts.telegramNotice}
      </p>

      {canEdit && (
        <form
          onSubmit={handleSubmit}
          className="flex flex-col gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4"
        >
          <h2 className="text-sm font-semibold">{t.alerts.addTitle}</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.alerts.name}</label>
              <input
                required
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t.alerts.namePlaceholder}
                className="aruzor-input"
              />
            </div>
            <div className="flex flex-col gap-1 sm:col-span-2">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.alerts.promql}</label>
              <input
                required
                value={promql}
                onChange={(e) => setPromql(e.target.value)}
                placeholder={t.alerts.promqlPlaceholder}
                className="aruzor-input font-mono"
              />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.alerts.operator}</label>
              <select
                value={operator}
                onChange={(e) => setOperator(e.target.value as AlertOperator)}
                className="aruzor-select"
              >
                {OPERATORS.map((op) => (
                  <option key={op} value={op}>
                    {op}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.alerts.threshold}</label>
              <input
                type="number"
                required
                value={threshold}
                onChange={(e) => setThreshold(e.target.value)}
                className="aruzor-input"
              />
            </div>
          </div>
          <button
            type="submit"
            disabled={submitting}
            className="mt-1 w-fit rounded-md bg-[var(--color-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-60"
          >
            {t.alerts.add}
          </button>
        </form>
      )}

      {error && <p className="text-sm text-[var(--color-danger)]">{error}</p>}

      <div className="aruzor-table-wrap overflow-x-auto rounded-xl border border-[var(--color-border)] bg-[var(--color-card)]">
        <table className="aruzor-table w-full min-w-[720px] text-left text-sm">
          <thead>
            <tr className="border-b border-[var(--color-border)] text-xs text-[var(--color-text-muted)]">
              <th className="px-4 py-3 font-medium">{t.alerts.columns.name}</th>
              <th className="px-4 py-3 font-medium">{t.alerts.columns.state}</th>
              <th className="px-4 py-3 font-medium">{t.alerts.columns.threshold}</th>
              <th className="px-4 py-3 font-medium">{t.alerts.columns.actions}</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td className="px-4 py-4 text-[var(--color-text-muted)]" colSpan={4}>
                  {t.dashboard.loading}
                </td>
              </tr>
            ) : rules.length === 0 ? (
              <tr>
                <td className="px-4 py-4 text-[var(--color-text-muted)]" colSpan={4}>
                  {t.alerts.empty}
                </td>
              </tr>
            ) : (
              rules.map((rule) => (
                <Fragment key={rule.id}>
                  <tr className="border-b border-[var(--color-border)] last:border-0">
                    <td className="px-4 py-2" data-label={t.alerts.columns.name}>
                      <div className="font-medium">{rule.name}</div>
                      <div className="aruzor-code font-mono text-xs text-[var(--color-text-muted)]">{rule.promql}</div>
                    </td>
                    <td className="px-4 py-2" data-label={t.alerts.columns.state}>
                      <div className="flex flex-wrap items-center gap-1">
                        <span
                          className="rounded-full px-2 py-0.5 text-xs font-medium"
                          style={{
                            color: rule.lastState === "firing" ? "var(--color-danger)" : "var(--color-success)",
                            backgroundColor:
                              rule.lastState === "firing"
                                ? "color-mix(in srgb, var(--color-danger) 15%, transparent)"
                                : "color-mix(in srgb, var(--color-success) 15%, transparent)",
                          }}
                        >
                          {rule.lastState === "firing" ? t.alerts.state.firing : t.alerts.state.ok}
                        </span>
                        {isSnoozed(rule) && (
                          <span className="rounded-full bg-[var(--color-warning)]/15 px-2 py-0.5 text-xs text-[var(--color-warning)]">
                            {t.alerts.snoozedBadge}
                          </span>
                        )}
                        {rule.lastState === "firing" && rule.ackedAt && (
                          <span className="rounded-full bg-[var(--color-border)] px-2 py-0.5 text-xs text-[var(--color-text-muted)]">
                            {t.alerts.ackedBadge}
                          </span>
                        )}
                      </div>
                    </td>
                    <td className="px-4 py-2 text-[var(--color-text-muted)]" data-label={t.alerts.columns.threshold}>
                      {rule.operator} {rule.threshold}
                    </td>
                    <td className="px-4 py-2" data-label={t.alerts.columns.actions}>
                      <div className="flex flex-wrap gap-2">
                        {canEdit && (
                          <>
                            <button
                              onClick={() => toggle(rule)}
                              className="rounded-md border border-[var(--color-border)] px-2 py-1 text-xs hover:bg-[var(--color-border)]/30"
                            >
                              {rule.enabled ? t.alerts.disable : t.alerts.enable}
                            </button>
                            {rule.lastState === "firing" && !rule.ackedAt && (
                              <button
                                onClick={() => ack(rule)}
                                className="rounded-md border border-[var(--color-border)] px-2 py-1 text-xs hover:bg-[var(--color-border)]/30"
                              >
                                {t.alerts.ack}
                              </button>
                            )}
                            {isSnoozed(rule) ? (
                              <button
                                onClick={() => snooze(rule, 0)}
                                className="rounded-md border border-[var(--color-border)] px-2 py-1 text-xs hover:bg-[var(--color-border)]/30"
                              >
                                {t.alerts.unsnooze}
                              </button>
                            ) : (
                              <select
                                defaultValue=""
                                onChange={(e) => {
                                  const minutes = Number(e.target.value);
                                  if (minutes > 0) snooze(rule, minutes);
                                  e.target.value = "";
                                }}
                                className="aruzor-select"
                              >
                                <option value="" disabled>
                                  {t.alerts.snooze}
                                </option>
                                {SNOOZE_OPTIONS.map((o) => (
                                  <option key={o.minutes} value={o.minutes}>
                                    {t.alerts[o.key]}
                                  </option>
                                ))}
                              </select>
                            )}
                            <button
                              onClick={() => setPendingDelete(rule)}
                              className="rounded-md border border-[var(--color-border)] px-2 py-1 text-xs text-[var(--color-danger)] hover:bg-[var(--color-danger)]/10"
                            >
                              {t.alerts.delete}
                            </button>
                          </>
                        )}
                        <button
                          onClick={() => toggleHistory(rule)}
                          className="rounded-md border border-[var(--color-border)] px-2 py-1 text-xs hover:bg-[var(--color-border)]/30"
                        >
                          {historyOpenFor === rule.id ? t.alerts.hideHistory : t.alerts.showHistory}
                        </button>
                      </div>
                    </td>
                  </tr>
                  {historyOpenFor === rule.id && (
                    <tr className="border-b border-[var(--color-border)] last:border-0">
                      <td colSpan={4} className="bg-[var(--color-bg)] px-4 py-3">
                        {historyLoading ? (
                          <p className="text-xs text-[var(--color-text-muted)]">{t.dashboard.loading}</p>
                        ) : historyEvents.length === 0 ? (
                          <p className="text-xs text-[var(--color-text-muted)]">{t.alerts.historyEmpty}</p>
                        ) : (
                          <div className="flex flex-col gap-3">
                            <AlertHistoryTimeline events={historyEvents} now={now ?? Date.parse(historyEvents[0].createdAt)} />
                            <ul className="flex flex-col gap-1 text-xs">
                            {historyEvents.map((ev) => (
                              <li key={ev.id} className="flex items-center gap-2 text-[var(--color-text-muted)]">
                                <span>{formatDateTime(ev.createdAt)}</span>
                                <span className={ev.event === "fired" ? "text-[var(--color-danger)]" : "text-[var(--color-success)]"}>
                                  {ev.event === "fired" ? t.alerts.state.firing : t.alerts.state.ok}
                                </span>
                                <span>({ev.value})</span>
                              </li>
                              ))}
                            </ul>
                          </div>
                        )}
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))
            )}
          </tbody>
        </table>
      </div>

      <ConfirmDialog
        open={pendingDelete !== null}
        title={t.alerts.confirmDeleteTitle}
        message={t.alerts.confirmDeleteMessage}
        confirmLabel={t.alerts.confirm}
        cancelLabel={t.alerts.cancel}
        onConfirm={confirmDelete}
        onCancel={() => setPendingDelete(null)}
      />
    </div>
  );
}
