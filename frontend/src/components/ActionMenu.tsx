"use client";

import { useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n/context";

export type Action = {
  key: string;
  label: string;
  onClick: () => void;
  danger?: boolean;
};

// Secondary toolbar actions, declared once and rendered for whichever shape
// the screen is. On a wide screen they are buttons in a row, because there
// is room and one click beats two. On a phone that row wrapped onto three
// lines and pushed the first chart most of a screen down, so the same
// actions collapse behind a single overflow control instead.
//
// The list is data rather than two copies of the same JSX: duplicated
// markup is how a button ends up on one layout and not the other.
export function ActionMenu({ actions }: { actions: Action[] }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  if (actions.length === 0) return null;

  return (
    <>
      <div className="hidden items-center gap-2 md:flex">
        {actions.map((a) => (
          <button
            key={a.key}
            onClick={a.onClick}
            className={`rounded-md border border-[var(--color-border)] px-3 py-1.5 text-sm hover:bg-[var(--color-border)]/30 ${
              a.danger ? "text-[var(--color-danger)] hover:bg-[var(--color-danger)]/10" : ""
            }`}
          >
            {a.label}
          </button>
        ))}
      </div>

      <button
        onClick={() => setOpen(true)}
        aria-label={t.common.actions}
        aria-expanded={open}
        className="flex items-center justify-center rounded-md border border-[var(--color-border)] px-3 text-[var(--color-text-muted)] md:hidden"
      >
        <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
          <circle cx="5" cy="12" r="1.5" fill="currentColor" />
          <circle cx="12" cy="12" r="1.5" fill="currentColor" />
          <circle cx="19" cy="12" r="1.5" fill="currentColor" />
        </svg>
      </button>

      {open && (
        <div className="fixed inset-0 z-40 md:hidden">
          <button className="absolute inset-0 bg-black/50" aria-label={t.common.close} onClick={() => setOpen(false)} />
          <div className="aruzor-sheet absolute inset-x-0 bottom-0 max-h-[85vh] overflow-y-auto rounded-t-2xl border-t border-[var(--color-border)] bg-[var(--color-card)]">
            <div className="flex justify-center pt-2 pb-1">
              <span className="h-1 w-10 rounded-full bg-[var(--color-border)]" />
            </div>
            <p className="px-5 pb-2 text-base font-semibold">{t.common.actions}</p>
            <div className="flex flex-col px-3 pb-3">
              {actions.map((a) => (
                <button
                  key={a.key}
                  onClick={() => {
                    setOpen(false);
                    a.onClick();
                  }}
                  className={`rounded-xl px-3 py-3 text-left text-[15px] font-medium ${
                    a.danger ? "text-[var(--color-danger)]" : "text-[var(--color-text)]"
                  }`}
                >
                  {a.label}
                </button>
              ))}
            </div>
          </div>
        </div>
      )}
    </>
  );
}
