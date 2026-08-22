const ROLE_RANK: Record<string, number> = { viewer: 0, editor: 1, admin: 2, super_admin: 3 };

// Mirrors the backend's roleRank in internal/api/router.go — keep in sync.
export function hasMinRole(role: string | undefined, min: string): boolean {
  return (ROLE_RANK[role ?? ""] ?? -1) >= (ROLE_RANK[min] ?? 0);
}
