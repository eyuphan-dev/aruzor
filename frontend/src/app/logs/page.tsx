"use client";

import { useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n/context";
import { useAuth } from "@/lib/auth/context";
import { listAuditLogs, deleteAuditLogs, type AuditLog } from "@/lib/api";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { formatDateTime } from "@/lib/format";

// Events that indicate a possible attack or misuse attempt, rather than
// routine activity — highlighted so a super_admin can spot them at a
// glance instead of reading every row.
const THREAT_EVENTS = new Set(["login_failed", "login_rate_limited", "forbidden_attempt", "login_new_ip"]);
const HIGH_SEVERITY_EVENTS = new Set(["login_rate_limited", "forbidden_attempt"]);

export default function LogsPage() {
  const { t } = useI18n();
  const { session } = useAuth();
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [confirmingDeleteAll, setConfirmingDeleteAll] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [threatsOnly, setThreatsOnly] = useState(false);

  const refresh = () => {
    setLoading(true);
    listAuditLogs()
      .then(setLogs)
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false));
  };

  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(refresh, []);

  if (session && session.role !== "super_admin") {
    return <p className="text-sm text-[var(--color-text-muted)]">{t.common.forbidden}</p>;
  }

  const filteredLogs = threatsOnly ? logs.filter((l) => THREAT_EVENTS.has(l.event)) : logs;

  const runDeleteAll = async () => {
    try {
      const res = await deleteAuditLogs("all");
      setNotice(`${res.deleted} ${t.logs.deleted}`);
      refresh();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setConfirmingDeleteAll(false);
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="aruzor-page-title text-xl font-semibold">{t.logs.title}</h1>
        <p className="text-sm text-[var(--color-text-muted)]">{t.logs.subtitle}</p>
      </div>

      <div className="flex flex-wrap items-center gap-4">
        <button
          onClick={() => setConfirmingDeleteAll(true)}
          className="rounded-md bg-[var(--color-danger)] px-3 py-1.5 text-sm font-medium text-white hover:opacity-90"
        >
          {t.logs.deleteAll}
        </button>
        <label className="flex items-center gap-2 text-sm text-[var(--color-text-muted)]">
          <input
            type="checkbox"
            checked={threatsOnly}
            onChange={(e) => setThreatsOnly(e.target.checked)}
            className="h-5 w-5 accent-[var(--color-primary)]"
          />
          {t.logs.threatsOnly}
        </label>
      </div>

      {notice && <p className="text-sm text-[var(--color-success)]">{notice}</p>}
      {error && <p className="text-sm text-[var(--color-danger)]">{error}</p>}

      <div className="aruzor-table-wrap overflow-x-auto rounded-xl border border-[var(--color-border)] bg-[var(--color-card)]">
        <table className="aruzor-table w-full min-w-[600px] text-left text-sm">
          <thead>
            <tr className="border-b border-[var(--color-border)] text-xs text-[var(--color-text-muted)]">
              <th className="px-4 py-3 font-medium">{t.logs.columns.time}</th>
              <th className="px-4 py-3 font-medium">{t.logs.columns.email}</th>
              <th className="px-4 py-3 font-medium">{t.logs.columns.event}</th>
              <th className="px-4 py-3 font-medium">{t.logs.columns.detail}</th>
              <th className="px-4 py-3 font-medium">{t.logs.columns.remote}</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td className="px-4 py-4 text-[var(--color-text-muted)]" colSpan={5}>
                  {t.dashboard.loading}
                </td>
              </tr>
            ) : filteredLogs.length === 0 ? (
              <tr>
                <td className="px-4 py-4 text-[var(--color-text-muted)]" colSpan={5}>
                  {t.logs.empty}
                </td>
              </tr>
            ) : (
              filteredLogs.map((l) => {
                const isThreat = THREAT_EVENTS.has(l.event);
                const isHighSeverity = HIGH_SEVERITY_EVENTS.has(l.event);
                const label = t.logs.events[l.event as keyof typeof t.logs.events] ?? l.event;
                return (
                  <tr
                    key={l.id}
                    className={`border-b border-[var(--color-border)] last:border-0 ${isThreat ? "bg-[var(--color-danger)]/5" : ""}`}
                  >
                    <td className="px-4 py-2 text-[var(--color-text-muted)]" data-label={t.logs.columns.time}>
                      {formatDateTime(l.createdAt)}
                    </td>
                    <td className="px-4 py-2" data-label={t.logs.columns.email}>{l.email}</td>
                    <td className="px-4 py-2" data-label={t.logs.columns.event}>
                      <span
                        className={`rounded-full px-2 py-0.5 text-xs font-medium ${
                          isHighSeverity
                            ? "bg-[var(--color-danger)]/15 text-[var(--color-danger)]"
                            : isThreat
                              ? "bg-[var(--color-warning)]/15 text-[var(--color-warning)]"
                              : "text-[var(--color-text-muted)]"
                        }`}
                      >
                        {label}
                      </span>
                    </td>
                    <td
                      className="max-w-[260px] px-4 py-2 text-xs text-[var(--color-text-muted)]"
                      data-label={t.logs.columns.detail}
                    >
                      {l.detail ? (
                        <span className="block truncate font-mono" title={l.detail}>
                          {l.detail}
                        </span>
                      ) : (
                        "—"
                      )}
                    </td>
                    <td className="px-4 py-2 text-[var(--color-text-muted)]" data-label={t.logs.columns.remote}>{l.remoteAddr}</td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>

      <ConfirmDialog
        open={confirmingDeleteAll}
        title={t.logs.confirmTitle}
        message={t.logs.confirmAllMessage}
        confirmLabel={t.logs.confirm}
        cancelLabel={t.logs.cancel}
        onConfirm={runDeleteAll}
        onCancel={() => setConfirmingDeleteAll(false)}
      />
    </div>
  );
}
