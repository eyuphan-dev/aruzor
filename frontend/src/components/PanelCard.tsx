export function PanelCard({
  title,
  status,
  children,
}: {
  title: string;
  status?: "ok" | "warn" | "error";
  children: React.ReactNode;
}) {
  const statusColor =
    status === "error" ? "var(--color-danger)" : status === "warn" ? "var(--color-warning)" : "var(--color-success)";

  return (
    <div className="flex h-full flex-col rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-sm font-medium text-[var(--color-text)]">{title}</h3>
        {status && <span className="h-2 w-2 rounded-full" style={{ backgroundColor: statusColor }} />}
      </div>
      <div className="min-h-0 flex-1">{children}</div>
    </div>
  );
}
