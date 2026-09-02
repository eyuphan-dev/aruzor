"use client";

import { useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n/context";
import { useAuth } from "@/lib/auth/context";
import { listUsers, createUser, updateUserRole, resetUserPassword, deleteUser, type AppUser, type AppRole } from "@/lib/api";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { formatDateTime } from "@/lib/format";

const ROLES: AppRole[] = ["viewer", "editor", "admin", "super_admin"];

export default function UsersPage() {
  const { t } = useI18n();
  const { session } = useAuth();
  const [users, setUsers] = useState<AppUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<AppUser | null>(null);
  const [roleSaving, setRoleSaving] = useState<string | null>(null);
  const [resettingId, setResettingId] = useState<string | null>(null);
  const [newPassword, setNewPassword] = useState("");
  const [resetSubmitting, setResetSubmitting] = useState(false);

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<AppRole>("viewer");
  const [submitting, setSubmitting] = useState(false);

  const refresh = () => {
    setLoading(true);
    listUsers()
      .then(setUsers)
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false));
  };

  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(refresh, []);

  if (session && session.role !== "super_admin") {
    return <p className="text-sm text-[var(--color-text-muted)]">{t.common.forbidden}</p>;
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      await createUser({ email, password, role });
      setEmail("");
      setPassword("");
      setRole("viewer");
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
      await deleteUser(pendingDelete.id);
      refresh();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setPendingDelete(null);
    }
  };

  const changeRole = async (user: AppUser, role: AppRole) => {
    if (role === user.role) return;
    setRoleSaving(user.id);
    setError(null);
    try {
      await updateUserRole(user.id, role);
      refresh();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setRoleSaving(null);
    }
  };

  const startReset = (user: AppUser) => {
    setResettingId(user.id);
    setNewPassword("");
  };

  const submitReset = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!resettingId) return;
    setResetSubmitting(true);
    setError(null);
    try {
      await resetUserPassword(resettingId, newPassword);
      setResettingId(null);
      setNewPassword("");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setResetSubmitting(false);
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="aruzor-page-title text-xl font-semibold">{t.users.title}</h1>
        <p className="text-sm text-[var(--color-text-muted)]">{t.users.subtitle}</p>
      </div>

      <form
        onSubmit={handleSubmit}
        className="flex flex-col gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4"
      >
        <h2 className="text-sm font-semibold">{t.users.addTitle}</h2>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <div className="flex flex-col gap-1">
            <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.users.email}</label>
            <input
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="aruzor-input"
            />
          </div>
          <div className="flex flex-col gap-1">
            <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.users.password}</label>
            <input
              type="password"
              required
              minLength={8}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={t.users.passwordHint}
              className="aruzor-input"
            />
          </div>
          <div className="flex flex-col gap-1">
            <label className="text-xs font-medium text-[var(--color-text-muted)]">{t.users.role}</label>
            <select
              value={role}
              onChange={(e) => setRole(e.target.value as AppRole)}
              className="aruzor-select"
            >
              {ROLES.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </div>
        </div>
        <button
          type="submit"
          disabled={submitting}
          className="mt-1 w-fit rounded-md bg-[var(--color-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-60"
        >
          {t.users.add}
        </button>
      </form>

      {error && <p className="text-sm text-[var(--color-danger)]">{error}</p>}

      <div className="aruzor-table-wrap overflow-x-auto rounded-xl border border-[var(--color-border)] bg-[var(--color-card)]">
        <table className="aruzor-table w-full min-w-[600px] text-left text-sm">
          <thead>
            <tr className="border-b border-[var(--color-border)] text-xs text-[var(--color-text-muted)]">
              <th className="px-4 py-3 font-medium">{t.users.columns.email}</th>
              <th className="px-4 py-3 font-medium">{t.users.columns.role}</th>
              <th className="px-4 py-3 font-medium">{t.users.columns.createdAt}</th>
              <th className="px-4 py-3 font-medium">{t.users.columns.actions}</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td className="px-4 py-4 text-[var(--color-text-muted)]" colSpan={4}>
                  {t.dashboard.loading}
                </td>
              </tr>
            ) : users.length === 0 ? (
              <tr>
                <td className="px-4 py-4 text-[var(--color-text-muted)]" colSpan={4}>
                  {t.users.empty}
                </td>
              </tr>
            ) : (
              users.map((u) => {
                const isSelf = u.id === session?.userId;
                return (
                  <tr key={u.id} className="border-b border-[var(--color-border)] last:border-0">
                    <td className="px-4 py-2" data-label={t.users.columns.email}>{u.email}</td>
                    <td className="px-4 py-2" data-label={t.users.columns.role}>
                      <select
                        value={u.role}
                        disabled={roleSaving === u.id}
                        onChange={(e) => changeRole(u, e.target.value as AppRole)}
                        title={isSelf ? t.users.roleChangeSelfHint : undefined}
                        className="aruzor-select"
                      >
                        {ROLES.map((r) => (
                          <option key={r} value={r}>
                            {r}
                          </option>
                        ))}
                      </select>
                    </td>
                    <td className="px-4 py-2 text-[var(--color-text-muted)]" data-label={t.users.columns.createdAt}>
                      {formatDateTime(u.createdAt)}
                    </td>
                    <td className="px-4 py-2" data-label={t.users.columns.actions}>
                      <div className="flex flex-wrap gap-2">
                        <button
                          onClick={() => startReset(u)}
                          className="min-h-9 rounded-md border border-[var(--color-border)] px-3 py-1.5 text-xs hover:bg-[var(--color-border)]/30"
                        >
                          {t.users.resetPassword}
                        </button>
                        <button
                          onClick={() => setPendingDelete(u)}
                          disabled={isSelf}
                          title={isSelf ? t.users.cannotDeleteSelf : undefined}
                          className="min-h-9 rounded-md border border-[var(--color-border)] px-3 py-1.5 text-xs text-[var(--color-danger)] hover:bg-[var(--color-danger)]/10 disabled:cursor-not-allowed disabled:opacity-40"
                        >
                          {t.users.delete}
                        </button>
                      </div>
                      {resettingId === u.id && (
                        <form
                          onSubmit={submitReset}
                          className="mt-2 flex flex-col gap-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-bg)] p-2 sm:flex-row sm:items-center"
                        >
                          <input
                            type="password"
                            required
                            minLength={8}
                            autoFocus
                            value={newPassword}
                            onChange={(e) => setNewPassword(e.target.value)}
                            placeholder={t.users.passwordHint}
                            className="aruzor-input w-full sm:min-w-[160px] sm:flex-1"
                          />
                          <div className="flex gap-2">
                            <button
                              type="submit"
                              disabled={resetSubmitting}
                              className="min-h-9 flex-1 rounded-md bg-[var(--color-primary)] px-3 py-1.5 text-xs font-medium text-white hover:opacity-90 disabled:opacity-60 sm:flex-none"
                            >
                              {t.users.saveEdit}
                            </button>
                            <button
                              type="button"
                              onClick={() => setResettingId(null)}
                              className="min-h-9 flex-1 rounded-md border border-[var(--color-border)] px-3 py-1.5 text-xs hover:bg-[var(--color-border)]/30 sm:flex-none"
                            >
                              {t.dashboard.cancel}
                            </button>
                          </div>
                        </form>
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
        title={t.users.confirmDeleteTitle}
        message={t.users.confirmDeleteMessage}
        confirmLabel={t.users.confirm}
        cancelLabel={t.users.cancel}
        onConfirm={confirmDelete}
        onCancel={() => setPendingDelete(null)}
      />
    </div>
  );
}
