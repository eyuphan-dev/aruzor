"use client";

import { use, useEffect, useState } from "react";
import Image from "next/image";
import { useI18n } from "@/lib/i18n/context";
import { DashboardPanel } from "@/components/DashboardPanel";
import { TimeRangeControls } from "@/components/TimeRangeControls";
import { getSharedDashboard, type DashboardDefinition } from "@/lib/api";

// The public, read-only view behind a share link. Rendered outside the app
// shell on purpose: a viewer with no account has no navigation to offer, and
// showing a sidebar full of pages that would all reject them is worse than
// showing none.

const RANGE_STORAGE_KEY = "aruzor.shared.range";

export default function SharedDashboardPage({ params }: { params: Promise<{ token: string }> }) {
  const { token } = use(params);
  const { t } = useI18n();

  const [title, setTitle] = useState("");
  const [def, setDef] = useState<DashboardDefinition | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const [rangeSeconds, setRangeSeconds] = useState(3600);
  const [refreshMs, setRefreshMs] = useState(30_000);
  const [refreshNonce, setRefreshNonce] = useState(0);

  useEffect(() => {
    const stored = window.localStorage.getItem(RANGE_STORAGE_KEY);
    if (stored) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setRangeSeconds(Number(stored));
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    getSharedDashboard(token)
      .then((d) => {
        if (cancelled) return;
        setTitle(d.title);
        setDef(d.definition);
      })
      .catch(() => {
        // Revoked and never-existed are the same answer from the server, and
        // they are the same answer here too.
        if (!cancelled) setError(t.dashboard.sharedNotFound);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [token, t.dashboard.sharedNotFound]);

  const changeRange = (next: number) => {
    setRangeSeconds(next);
    window.localStorage.setItem(RANGE_STORAGE_KEY, String(next));
  };

  return (
    <div className="min-h-screen w-full bg-[var(--color-bg)]">
      <header className="flex flex-wrap items-center gap-3 border-b border-[var(--color-border)] bg-[var(--color-card)] px-4 py-3 sm:px-6">
        <div className="flex items-center gap-2">
          <Image src="/logo-mark.png" alt="" width={26} height={26} className="rounded-md" priority />
          <span className="font-semibold tracking-tight">{t.appName}</span>
        </div>
        {title && <span className="text-sm text-[var(--color-text-muted)]">· {title}</span>}
        <span className="rounded-full border border-[var(--color-border)] px-2 py-0.5 text-xs text-[var(--color-text-muted)]">
          {t.dashboard.sharedReadOnly}
        </span>
        {def && (
          <div className="ml-auto">
            <TimeRangeControls
              rangeSeconds={rangeSeconds}
              onRangeChange={changeRange}
              refreshMs={refreshMs}
              onRefreshMsChange={setRefreshMs}
              onRefreshNow={() => setRefreshNonce((n) => n + 1)}
            />
          </div>
        )}
      </header>

      <main className="p-4 sm:p-6">
        {loading ? (
          <p className="text-sm text-[var(--color-text-muted)]">{t.dashboard.loading}</p>
        ) : error ? (
          <p className="text-sm text-[var(--color-danger)]">{error}</p>
        ) : !def || def.panels.length === 0 ? (
          <p className="text-sm text-[var(--color-text-muted)]">{t.dashboard.noData}</p>
        ) : (
          // A plain responsive grid rather than the draggable one: nothing
          // here can be rearranged, so the drag machinery would only cost the
          // viewer a bundle they cannot use.
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            {def.panels.map((p) => (
              <div key={p.id} className="h-[300px]">
                <DashboardPanel
                  title={p.title}
                  promQL={p.promql}
                  color={p.color}
                  panelType={p.panelType}
                  threshold={p.threshold}
                  annotations={def.annotations}
                  rangeSeconds={rangeSeconds}
                  refreshMs={refreshMs}
                  refreshNonce={refreshNonce}
                  editable={false}
                  shareToken={token}
                  onRemove={() => undefined}
                  onDuplicate={() => undefined}
                  onChangeType={() => undefined}
                />
              </div>
            ))}
          </div>
        )}
      </main>
    </div>
  );
}
