"use client";

import { useEffect, useRef, useState } from "react";
import { useI18n } from "@/lib/i18n/context";
import { useAuth } from "@/lib/auth/context";
import { getSettings, updateSetting, updateSettingValue, type Settings } from "@/lib/api";
import { createBackup, parseBackup, restoreBackup, type RestoreReport } from "@/lib/backup";

export default function SettingsPage() {
  const { t } = useI18n();
  const { session } = useAuth();
  const [settings, setSettings] = useState<Settings | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [saving, setSaving] = useState<string | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState<"export" | "import" | null>(null);
  const [report, setReport] = useState<RestoreReport | null>(null);
  const [webhookUrl, setWebhookUrl] = useState("");
  const [savingWebhook, setSavingWebhook] = useState(false);


  useEffect(() => {
    getSettings()
      .then((s) => {
        setSettings(s);
        setWebhookUrl(s.webhook_url ?? "");
      })
      .catch((err: Error) => setError(err.message));
  }, []);

  if (session && session.role !== "super_admin") {
    return <p className="text-sm text-[var(--color-text-muted)]">{t.common.forbidden}</p>;
  }

  const handleExport = async () => {
    setBusy("export");
    setError(null);
    setReport(null);
    try {
      const backup = await createBackup();
      const blob = new Blob([JSON.stringify(backup, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `aruzor-yedek-${new Date().toISOString().slice(0, 10)}.json`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(null);
    }
  };

  const handleImport = async (file: File) => {
    setBusy("import");
    setError(null);
    setReport(null);
    try {
      const backup = parseBackup(await file.text());
      setReport(await restoreBackup(backup));
    } catch (err) {
      const reason = (err as Error).message;
      setError(
        reason === "invalid-json"
          ? t.settingsPage.backupBadJson
          : reason === "unsupported-version"
            ? t.settingsPage.backupUnsupported
            : reason === "not-a-backup"
              ? t.settingsPage.backupInvalid
              : reason,
      );
    } finally {
      setBusy(null);
      // Cleared so choosing the same file again still fires a change event.
      if (fileInput.current) fileInput.current.value = "";
    }
  };

  const saveWebhook = async () => {
    setSavingWebhook(true);
    setError(null);
    try {
      await updateSettingValue("webhook_url", webhookUrl.trim());
      setSettings((prev) => (prev ? { ...prev, webhook_url: webhookUrl.trim() } : prev));
      setNotice(t.settingsPage.saved);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSavingWebhook(false);
    }
  };

  const toggle = async (key: string, next: boolean) => {
    setSaving(key);
    setError(null);
    try {
      await updateSetting(key, next);
      setSettings((prev) => (prev ? { ...prev, [key]: next ? "true" : "false" } : prev));
      setNotice(t.settingsPage.saved);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSaving(null);
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="aruzor-page-title text-xl font-semibold">{t.settingsPage.title}</h1>
        <p className="text-sm text-[var(--color-text-muted)]">{t.settingsPage.subtitle}</p>
      </div>

      {notice && <p className="text-sm text-[var(--color-success)]">{notice}</p>}
      {error && <p className="text-sm text-[var(--color-danger)]">{error}</p>}

      {!settings ? (
        <p className="text-sm text-[var(--color-text-muted)]">{t.dashboard.loading}</p>
      ) : (
        <div className="flex flex-col gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
          <div className="flex items-center justify-between gap-4">
            <div>
              <div className="text-sm font-medium">{t.settingsPage.statusPageEnabled}</div>
              <div className="text-xs text-[var(--color-text-muted)]">{t.settingsPage.statusPageEnabledHint}</div>
            </div>
            <button
              role="switch"
              aria-checked={settings.status_page_enabled === "true"}
              disabled={saving === "status_page_enabled"}
              onClick={() => toggle("status_page_enabled", settings.status_page_enabled !== "true")}
              className={`relative my-1.5 h-6 w-11 shrink-0 rounded-full transition-colors before:absolute before:-inset-y-2 before:inset-x-0 before:content-[''] disabled:opacity-60 ${
                settings.status_page_enabled === "true" ? "bg-[var(--color-primary)]" : "bg-[var(--color-border)]"
              }`}
            >
              <span
                className={`absolute top-0.5 h-5 w-5 rounded-full bg-white transition-transform ${
                  settings.status_page_enabled === "true" ? "translate-x-5" : "translate-x-0.5"
                }`}
              />
            </button>
          </div>
          {settings.status_page_enabled === "true" && (
            <a
              href="/status"
              target="_blank"
              rel="noreferrer"
              className="w-fit text-xs text-[var(--color-primary)] underline underline-offset-2"
            >
              /status →
            </a>
          )}
        </div>
      )}

      {settings && (
        <div className="flex flex-col gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
          <div>
            <div className="text-sm font-medium">{t.settingsPage.webhookTitle}</div>
            <div className="text-xs text-[var(--color-text-muted)]">{t.settingsPage.webhookHint}</div>
          </div>
          <div className="flex flex-wrap gap-2">
            <input
              type="url"
              value={webhookUrl}
              onChange={(e) => setWebhookUrl(e.target.value)}
              placeholder={t.settingsPage.webhookPlaceholder}
              className="aruzor-input min-w-0 flex-1"
            />
            <button
              onClick={saveWebhook}
              disabled={savingWebhook}
              className="min-h-9 shrink-0 rounded-md bg-[var(--color-primary)] px-4 text-sm font-medium text-white hover:opacity-90 disabled:opacity-60"
            >
              {t.settingsPage.webhookSave}
            </button>
          </div>
        </div>
      )}

      <div className="flex flex-col gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
        <div>
          <div className="text-sm font-medium">{t.settingsPage.backupTitle}</div>
          <div className="text-xs text-[var(--color-text-muted)]">{t.settingsPage.backupHint}</div>
        </div>

        <div className="flex flex-wrap gap-2">
          <button
            onClick={handleExport}
            disabled={busy !== null}
            className="min-h-9 rounded-md bg-[var(--color-primary)] px-4 text-sm font-medium text-white hover:opacity-90 disabled:opacity-60"
          >
            {busy === "export" ? t.settingsPage.backupExporting : t.settingsPage.backupExport}
          </button>
          <button
            onClick={() => fileInput.current?.click()}
            disabled={busy !== null}
            className="min-h-9 rounded-md border border-[var(--color-border)] px-4 text-sm font-medium hover:bg-[var(--color-border)]/30 disabled:opacity-60"
          >
            {busy === "import" ? t.settingsPage.backupImporting : t.settingsPage.backupImport}
          </button>
          <input
            ref={fileInput}
            type="file"
            accept="application/json,.json"
            className="hidden"
            onChange={(e) => {
              const file = e.target.files?.[0];
              if (file) handleImport(file);
            }}
          />
        </div>

        <p className="text-xs text-[var(--color-text-muted)]">{t.settingsPage.backupNote}</p>

        {report && (
          <div className="rounded-lg border border-[var(--color-border)] p-3 text-xs">
            <div className="mb-1 font-medium text-[var(--color-success)]">{t.settingsPage.backupResult}</div>
            <ul className="flex flex-col gap-0.5 text-[var(--color-text-muted)]">
              {(
                [
                  [t.settingsPage.backupDashboards, report.dashboards],
                  [t.settingsPage.backupAlerts, report.alerts],
                  [t.settingsPage.backupMonitors, report.monitors],
                  [t.settingsPage.backupDatasources, report.datasources],
                ] as const
              ).map(([label, counts]) => (
                <li key={label}>
                  {label}: {counts.created} {t.settingsPage.backupCreated}
                  {counts.skipped > 0 && `, ${counts.skipped} ${t.settingsPage.backupSkipped}`}
                </li>
              ))}
            </ul>
            {report.errors.length > 0 && (
              <ul className="mt-2 flex flex-col gap-0.5 text-[var(--color-danger)]">
                {report.errors.map((e) => (
                  <li key={e}>{e}</li>
                ))}
              </ul>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
