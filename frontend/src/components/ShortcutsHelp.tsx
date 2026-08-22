"use client";

import { useEffect } from "react";
import { useI18n } from "@/lib/i18n/context";

export type Shortcut = { keys: string; description: string };

export function ShortcutsHelp({ open, onClose, shortcuts }: { open: boolean; onClose: () => void; shortcuts: Shortcut[] }) {
  const { t } = useI18n();

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
      role="presentation"
    >
      <div
        className="w-full max-w-sm rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-5 shadow-xl"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label={t.dashboard.shortcutsTitle}
      >
        <h2 className="mb-3 text-sm font-semibold">{t.dashboard.shortcutsTitle}</h2>
        <div className="flex flex-col gap-1.5">
          {shortcuts.map((s) => (
            <div key={s.keys} className="flex items-center justify-between gap-4 text-sm">
              <span className="text-[var(--color-text-muted)]">{s.description}</span>
              <kbd className="rounded border border-[var(--color-border)] bg-[var(--color-bg)] px-1.5 py-0.5 font-mono text-xs">
                {s.keys}
              </kbd>
            </div>
          ))}
        </div>
        <button
          onClick={onClose}
          className="mt-4 w-full rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm hover:bg-[var(--color-bg)]"
        >
          {t.common.close}
        </button>
      </div>
    </div>
  );
}
