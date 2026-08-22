export type NavKey =
  | "dashboard"
  | "queryBuilder"
  | "alerts"
  | "datasources"
  | "monitors"
  | "users"
  | "logs"
  | "settings";

// Line icons at a shared 24-unit grid so they stay optically even when the
// sidebar collapses to an icon-only rail and the label is gone.
const paths: Record<NavKey, React.ReactNode> = {
  dashboard: (
    <>
      <rect x="3" y="3" width="7" height="9" rx="1.5" />
      <rect x="14" y="3" width="7" height="5" rx="1.5" />
      <rect x="14" y="12" width="7" height="9" rx="1.5" />
      <rect x="3" y="16" width="7" height="5" rx="1.5" />
    </>
  ),
  queryBuilder: (
    <>
      <path d="M4 6h16" />
      <path d="M7 12h13" />
      <path d="M10 18h10" />
      <circle cx="4" cy="12" r="1.4" />
      <circle cx="7" cy="18" r="1.4" />
    </>
  ),
  alerts: (
    <>
      <path d="M18 8a6 6 0 1 0-12 0c0 6-2 7-2 7h16s-2-1-2-7" />
      <path d="M10.5 19a2 2 0 0 0 3 0" />
    </>
  ),
  datasources: (
    <>
      <ellipse cx="12" cy="5.5" rx="7.5" ry="3" />
      <path d="M4.5 5.5v6c0 1.7 3.4 3 7.5 3s7.5-1.3 7.5-3v-6" />
      <path d="M4.5 11.5v6c0 1.7 3.4 3 7.5 3s7.5-1.3 7.5-3v-6" />
    </>
  ),
  monitors: (
    <>
      <path d="M3 12h4l2.5-6 4 12L16 12h5" />
    </>
  ),
  users: (
    <>
      <circle cx="9" cy="8" r="3.2" />
      <path d="M3.5 20a5.5 5.5 0 0 1 11 0" />
      <path d="M16 5.6a3.2 3.2 0 0 1 0 4.8" />
      <path d="M17.5 14.8A5.5 5.5 0 0 1 20.5 20" />
    </>
  ),
  logs: (
    <>
      <rect x="4" y="3" width="16" height="18" rx="2" />
      <path d="M8 8h8M8 12h8M8 16h5" />
    </>
  ),
  settings: (
    <>
      <circle cx="12" cy="12" r="3" />
      <path d="M12 2.5v2.2M12 19.3v2.2M21.5 12h-2.2M4.7 12H2.5M18.7 5.3l-1.6 1.6M6.9 17.1l-1.6 1.6M18.7 18.7l-1.6-1.6M6.9 6.9 5.3 5.3" />
    </>
  ),
};

export function NavIcon({ name, size = 18 }: { name: NavKey; size?: number }) {
  return (
    <svg
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className="shrink-0"
    >
      {paths[name]}
    </svg>
  );
}
