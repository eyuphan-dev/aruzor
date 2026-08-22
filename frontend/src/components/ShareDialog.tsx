"use client";

import { useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n/context";
import { createShareLink, getShareToken, revokeShareLink } from "@/lib/api";

export function ShareDialog({
  open,
  dashboardId,
  onClose,
}: {
  open: boolean;
  dashboardId: string;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const [token, setToken] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    // Opening the dialog is the point at which the previous run's state
    // stops being true, so it is reset here rather than left to linger.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLoading(true);
    setNotice(null);
    setError(null);
    getShareToken(dashboardId)
      .then((r) => setToken(r.token || null))
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false));
  }, [open, dashboardId]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;

  // Built from the current origin so the link works behind whatever domain
  // or port this instance is actually reached on.
  const url = token ? `${window.location.origin}/s/${token}` : "";

  const create = async () => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const r = await createShareLink(dashboardId);
      setToken(r.token);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const revoke = async () => {
    setBusy(true);
    setError(null);
    try {
      await revokeShareLink(dashboardId);
      setToken(null);
      setNotice(t.dashboard.shareRevoked);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard access needs a secure context; over plain HTTP the link is
      // still selectable in the input, so this is not worth an error banner.
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <button className="absolute inset-0 bg-black/40" aria-label={t.dashboard.shareClose} onClick={onClose} />
      <div className="relative w-full max-w-lg rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-5 shadow-xl">
        <h2 className="text-sm font-semibold">{t.dashboard.shareTitle}</h2>
        <p className="mt-1 text-xs text-[var(--color-text-muted)]">{t.dashboard.shareHint}</p>

        {loading ? (
          <p className="mt-4 text-sm text-[var(--color-text-muted)]">{t.dashboard.loading}</p>
        ) : token ? (
          <div className="mt-4 flex flex-col gap-3">
            <div className="flex flex-wrap gap-2">
              <input
                readOnly
                value={url}
                onFocus={(e) => e.currentTarget.select()}
                className="aruzor-input min-w-0 flex-1 font-mono text-xs"
              />
              <button
                onClick={copy}
                className="min-h-9 rounded-md bg-[var(--color-primary)] px-4 text-sm font-medium text-white hover:opacity-90"
              >
                {copied ? t.dashboard.shareCopied : t.dashboard.shareCopy}
              </button>
            </div>
            <button
              onClick={revoke}
              disabled={busy}
              className="w-fit min-h-9 rounded-md border border-[var(--color-border)] px-4 text-sm text-[var(--color-danger)] hover:bg-[var(--color-danger)]/10 disabled:opacity-60"
            >
              {t.dashboard.shareRevoke}
            </button>
          </div>
        ) : (
          <div className="mt-4 flex flex-col gap-3">
            <p className="text-sm text-[var(--color-text-muted)]">{t.dashboard.shareNone}</p>
            <button
              onClick={create}
              disabled={busy}
              className="w-fit min-h-9 rounded-md bg-[var(--color-primary)] px-4 text-sm font-medium text-white hover:opacity-90 disabled:opacity-60"
            >
              {busy ? t.dashboard.shareCreating : t.dashboard.shareCreate}
            </button>
          </div>
        )}

        {notice && <p className="mt-3 text-xs text-[var(--color-success)]">{notice}</p>}
        {error && <p className="mt-3 text-xs text-[var(--color-danger)]">{error}</p>}

        <button
          onClick={onClose}
          className="mt-5 min-h-9 w-full rounded-md border border-[var(--color-border)] text-sm hover:bg-[var(--color-border)]/30"
        >
          {t.dashboard.shareClose}
        </button>
      </div>
    </div>
  );
}
