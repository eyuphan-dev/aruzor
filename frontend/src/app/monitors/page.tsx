"use client";

import Link from "next/link";

import { useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n/context";
import { listMonitors, createMonitor, deleteMonitor, type Monitor, type MonitorType } from "@/lib/api";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { HelpTooltip } from "@/components/HelpTooltip";
import { useAuth } from "@/lib/auth/context";
import { hasMinRole } from "@/lib/auth/roles";

function isSnoozeActive(until?: string): boolean {
  return !!until && new Date(until).getTime() > Date.now();
}

export default function MonitorsPage() {
  const { t } = useI18n();
  const { session } = useAuth();
  const canEdit = hasMinRole(session?.role, "admin");
  const [monitors, setMonitors] = useState<Monitor[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<Monitor | null>(null);

  const [name, setName] = useState("");
  const [type, setType] = useState<MonitorType>("http");
  const [target, setTarget] = useState("");
  const [intervalSeconds, setIntervalSeconds] = useState("60");
  const [submitting, setSubmitting] = useState(false);

  const refresh = () => {
    setLoading(true);
    listMonitors()
      .then(setMonitors)
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false));
  };

  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(refresh, []);

  useEffect(() => {
    const id = setInterval(refresh, 30_000);
    return () => clearInterval(id);
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      await createMonitor({ name, type, target, intervalSeconds: Number(intervalSeconds) || 60 });
      setName("");
      setTarget("");
      refresh();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSubmitting(false);
    }
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    try {
      await deleteMonitor(pendingDelete.id);
      refresh();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setPendingDelete(null);
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="aruzor-page-title flex items-center gap-2 text-xl font-semibold">
          {t.monitors.title}
          <HelpTooltip text={t.monitors.helpText} />
        </h1>
        <p className="text-sm text-[var(--color-text-muted)]">{t.monitors.subtitle}</p>
      </div>

      {canEdit && (
        <form
          onSubmit={handleSubmit}
          className="flex flex-col gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4"
        >
          <h2 className="text-sm font-semibold">{t.monitors.addTitle}</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-4">
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.monitors.name}</label>
              <input
                required
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t.monitors.namePlaceholder}
                className="aruzor-input"
              />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.monitors.type}</label>
              <select
                value={type}
                onChange={(e) => setType(e.target.value as MonitorType)}
                className="aruzor-select"
              >
                <option value="http">HTTP</option>
                <option value="tcp">TCP</option>
              </select>
            </div>
            <div className="flex flex-col gap-1 sm:col-span-2">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.monitors.target}</label>
              <input
                required
                value={target}
                onChange={(e) => setTarget(e.target.value)}
                placeholder={type === "http" ? "https://example.com" : "10.0.0.5:22"}
                className="aruzor-input font-mono"
              />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.monitors.interval}</label>
              <input
                type="number"
                min={15}
                max={3600}
                value={intervalSeconds}
                onChange={(e) => setIntervalSeconds(e.target.value)}
                className="aruzor-input"
              />
            </div>
          </div>
          <button
            type="submit"
            disabled={submitting}
            className="mt-1 w-fit rounded-md bg-[var(--color-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-60"
          >
            {t.monitors.add}
          </button>
        </form>
      )}

      {error && <p className="text-sm text-[var(--color-danger)]">{error}</p>}

      <div className="aruzor-table-wrap overflow-x-auto rounded-xl border border-[var(--color-border)] bg-[var(--color-card)]">
        <table className="aruzor-table w-full min-w-[720px] text-left text-sm">
          <thead>
            <tr className="border-b border-[var(--color-border)] text-xs text-[var(--color-text-muted)]">
              <th className="px-4 py-3 font-medium">{t.monitors.columns.name}</th>
              <th className="px-4 py-3 font-medium">{t.monitors.columns.status}</th>
              <th className="px-4 py-3 font-medium">{t.monitors.columns.uptime}</th>
              <th className="px-4 py-3 font-medium">{t.monitors.columns.latency}</th>
              <th className="px-4 py-3 font-medium">{t.monitors.columns.actions}</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td className="px-4 py-4 text-[var(--color-text-muted)]" colSpan={5}>
                  {t.dashboard.loading}
                </td>
              </tr>
            ) : monitors.length === 0 ? (
              <tr>
                <td className="px-4 py-4 text-[var(--color-text-muted)]" colSpan={5}>
                  {t.monitors.empty}
                </td>
              </tr>
            ) : (
              monitors.map((m) => (
                <tr key={m.id} className="border-b border-[var(--color-border)] last:border-0">
                  <td className="px-4 py-2" data-label={t.monitors.columns.name}>
                    <Link href={`/monitors/${m.id}`} className="aruzor-tap group block">
                      <div className="font-medium text-[var(--color-primary)] group-hover:underline">{m.name}</div>
                      <div className="aruzor-code font-mono text-xs text-[var(--color-text-muted)]">
                        {m.type.toUpperCase()} · {m.target}
                      </div>
                    </Link>
                  </td>
                  <td className="px-4 py-2" data-label={t.monitors.columns.status}>
                    {m.lastOk === undefined ? (
                      <span className="text-xs text-[var(--color-text-muted)]">{t.monitors.pending}</span>
                    ) : (
                      <span
                        className="rounded-full px-2 py-0.5 text-xs font-medium"
                        style={{
                          color: m.lastOk ? "var(--color-success)" : "var(--color-danger)",
                          backgroundColor: m.lastOk
                            ? "color-mix(in srgb, var(--color-success) 15%, transparent)"
                            : "color-mix(in srgb, var(--color-danger) 15%, transparent)",
                        }}
                      >
                        {m.lastOk ? t.monitors.up : t.monitors.down}
                      </span>
                    )}
                    {isSnoozeActive(m.snoozedUntil) && (
                      <span className="ml-1.5 text-xs" title={t.monitors.snooze}>
                        🔕
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-2 text-[var(--color-text-muted)]" data-label={t.monitors.columns.uptime}>
                    {m.uptimePercent === undefined ? "—" : `%${m.uptimePercent.toFixed(1)}`}
                  </td>
                  <td className="px-4 py-2 text-[var(--color-text-muted)]" data-label={t.monitors.columns.latency}>
                    {m.lastLatencyMs === undefined ? "—" : `${m.lastLatencyMs} ms`}
                  </td>
                  <td className="px-4 py-2" data-label={t.monitors.columns.actions}>
                    {canEdit ? (
                      <button
                        onClick={() => setPendingDelete(m)}
                        className="rounded-md border border-[var(--color-border)] px-2 py-1 text-xs text-[var(--color-danger)] hover:bg-[var(--color-danger)]/10"
                      >
                        {t.monitors.delete}
                      </button>
                    ) : (
                      <span className="text-xs text-[var(--color-text-muted)]">—</span>
                    )}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      <ConfirmDialog
        open={pendingDelete !== null}
        title={t.monitors.confirmDeleteTitle}
        message={t.monitors.confirmDeleteMessage}
        confirmLabel={t.monitors.delete}
        cancelLabel={t.dashboard.cancel}
        onConfirm={confirmDelete}
        onCancel={() => setPendingDelete(null)}
      />
    </div>
  );
}
