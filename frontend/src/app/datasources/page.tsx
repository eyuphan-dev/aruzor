"use client";

import { useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n/context";
import { listDatasources, createDatasource, deleteDatasource, type Datasource } from "@/lib/api";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { HelpTooltip } from "@/components/HelpTooltip";
import { useAuth } from "@/lib/auth/context";
import { hasMinRole } from "@/lib/auth/roles";

export default function DatasourcesPage() {
  const { t } = useI18n();
  const { session } = useAuth();
  const canEdit = hasMinRole(session?.role, "admin");
  const [datasources, setDatasources] = useState<Datasource[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<Datasource | null>(null);

  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const refresh = () => {
    setLoading(true);
    listDatasources()
      .then(setDatasources)
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
      await createDatasource({ name, url });
      setName("");
      setUrl("");
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
      await deleteDatasource(pendingDelete.id);
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
          {t.datasources.title}
          <HelpTooltip text={t.datasources.helpText} />
        </h1>
        <p className="text-sm text-[var(--color-text-muted)]">{t.datasources.subtitle}</p>
      </div>

      {canEdit && (
      <form
        onSubmit={handleSubmit}
        className="flex flex-col gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4"
      >
        <h2 className="text-sm font-semibold">{t.datasources.addTitle}</h2>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="flex flex-col gap-1">
            <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.datasources.name}</label>
            <input
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t.datasources.namePlaceholder}
              className="aruzor-input"
            />
          </div>
          <div className="flex flex-col gap-1">
            <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.datasources.url}</label>
            <input
              required
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder={t.datasources.urlPlaceholder}
              className="aruzor-input font-mono"
            />
          </div>
        </div>
        <button
          type="submit"
          disabled={submitting}
          className="mt-1 w-fit rounded-md bg-[var(--color-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-60"
        >
          {t.datasources.add}
        </button>
      </form>
      )}

      {error && <p className="text-sm text-[var(--color-danger)]">{error}</p>}

      <div className="aruzor-table-wrap overflow-x-auto rounded-xl border border-[var(--color-border)] bg-[var(--color-card)]">
        <table className="aruzor-table w-full min-w-[600px] text-left text-sm">
          <thead>
            <tr className="border-b border-[var(--color-border)] text-xs text-[var(--color-text-muted)]">
              <th className="px-4 py-3 font-medium">{t.datasources.columns.name}</th>
              <th className="px-4 py-3 font-medium">{t.datasources.columns.url}</th>
              <th className="px-4 py-3 font-medium">{t.datasources.columns.type}</th>
              <th className="px-4 py-3 font-medium">{t.datasources.columns.actions}</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td className="px-4 py-4 text-[var(--color-text-muted)]" colSpan={4}>
                  {t.dashboard.loading}
                </td>
              </tr>
            ) : datasources.length === 0 ? (
              <tr>
                <td className="px-4 py-4 text-[var(--color-text-muted)]" colSpan={4}>
                  {t.datasources.empty}
                </td>
              </tr>
            ) : (
              datasources.map((ds) => {
                const isDefault = ds.id === "default";
                return (
                  <tr key={ds.id} className="border-b border-[var(--color-border)] last:border-0">
                    <td className="px-4 py-2" data-label={t.datasources.columns.name}>
                      <div className="flex items-center gap-2">
                        {ds.name}
                        {isDefault && (
                          <span className="rounded-full bg-[var(--color-primary)]/15 px-2 py-0.5 text-xs text-[var(--color-primary)]">
                            {t.datasources.defaultBadge}
                          </span>
                        )}
                      </div>
                    </td>
                    <td className="aruzor-code px-4 py-2 font-mono text-xs text-[var(--color-text-muted)]" data-label={t.datasources.columns.url}>{ds.url}</td>
                    <td className="px-4 py-2 text-[var(--color-text-muted)]" data-label={t.datasources.columns.type}>{ds.type}</td>
                    <td className="px-4 py-2" data-label={t.datasources.columns.actions}>
                      {canEdit ? (
                        <button
                          onClick={() => setPendingDelete(ds)}
                          disabled={isDefault}
                          title={isDefault ? t.datasources.cannotDeleteDefault : undefined}
                          className="min-h-9 rounded-md border border-[var(--color-border)] px-3 py-1.5 text-xs text-[var(--color-danger)] hover:bg-[var(--color-danger)]/10 disabled:cursor-not-allowed disabled:opacity-40"
                        >
                          {t.datasources.delete}
                        </button>
                      ) : (
                        <span className="text-xs text-[var(--color-text-muted)]">—</span>
                      )}
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>

      <ConfirmDialog
        open={pendingDelete !== null}
        title={t.datasources.delete}
        message={`${pendingDelete?.name ?? ""}`}
        confirmLabel={t.datasources.delete}
        cancelLabel={t.dashboard.cancel}
        onConfirm={confirmDelete}
        onCancel={() => setPendingDelete(null)}
      />
    </div>
  );
}
