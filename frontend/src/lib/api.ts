// Empty by default, which makes every request same-origin: "/api/v1/...".
// The frontend proxies /api to the backend (see next.config.ts rewrites),
// so one published port serves the whole app and nothing host-specific is
// baked into the bundle. Hard-coding an absolute address here is what
// breaks an install the moment it moves off localhost — set
// NEXT_PUBLIC_ARUZOR_API_URL only when the API really lives elsewhere, and
// then configure CORS to match.
const API_BASE = process.env.NEXT_PUBLIC_ARUZOR_API_URL ?? "";
const AUTH_STORAGE_KEY = "aruzor-auth";

export type PrometheusVectorResult = {
  resultType: string;
  result: Array<{
    metric: Record<string, string>;
    value?: [number, string];
    values?: [number, string][];
  }>;
};

export type LoginResult = { token: string; userId: string; email: string; role: string };

export type AuditLog = {
  id: string;
  userId?: string;
  email: string;
  event: string;
  remoteAddr: string;
  createdAt: string;
};

function authToken(): string | null {
  if (typeof window === "undefined") return null;
  const stored = window.localStorage.getItem(AUTH_STORAGE_KEY);
  if (!stored) return null;
  try {
    return (JSON.parse(stored) as LoginResult).token;
  } catch {
    return null;
  }
}

// Whether the last attempted request actually reached the server.
//
// navigator.onLine only reports whether the device is attached to a network,
// not whether anything is reachable over it — it stays true on a captive
// portal, on dead Wi-Fi, and after a reload made while offline. A request
// that failed at the transport layer is the honest signal, so that is what
// this tracks and what the UI listens to.
let reachable = true;
const reachabilityListeners = new Set<(ok: boolean) => void>();

export function onReachabilityChange(listener: (ok: boolean) => void): () => void {
  reachabilityListeners.add(listener);
  return () => reachabilityListeners.delete(listener);
}

export function isReachable(): boolean {
  return reachable;
}

function setReachable(next: boolean) {
  if (reachable === next) return;
  reachable = next;
  for (const listener of reachabilityListeners) listener(next);
}

async function apiRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const token = authToken();
  let res: Response;
  try {
    res = await fetch(`${API_BASE}${path}`, {
      ...init,
      cache: "no-store",
      headers: {
        ...(init?.body ? { "Content-Type": "application/json" } : {}),
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        ...init?.headers,
      },
    });
  } catch (err) {
    // fetch only rejects for transport failures; an HTTP error status
    // resolves normally and is handled below.
    setReachable(false);
    throw err;
  }
  setReachable(true);
  if (!res.ok) {
    // A 401 on an already-authenticated request means the session expired
    // or was invalidated server-side — bounce to /login instead of leaving
    // the user stuck seeing cryptic errors on every page/action.
    if (res.status === 401 && token && path !== "/api/v1/login" && typeof window !== "undefined") {
      window.localStorage.removeItem(AUTH_STORAGE_KEY);
      window.location.href = "/login";
    }
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error ?? `İstek başarısız: ${res.status}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

export function checkHealth() {
  return apiRequest<{ status: string }>("/api/v1/health");
}

export function login(email: string, password: string) {
  return apiRequest<LoginResult>("/api/v1/login", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });
}

export function getMetricNames(datasourceId?: string) {
  const params = datasourceId ? `?${new URLSearchParams({ datasource: datasourceId })}` : "";
  return apiRequest<string[]>(`/api/v1/metrics/names${params}`);
}

export function queryInstant(promQL: string, datasourceId?: string) {
  const params = new URLSearchParams({ query: promQL });
  if (datasourceId) params.set("datasource", datasourceId);
  return apiRequest<PrometheusVectorResult>(`/api/v1/query?${params}`);
}

// Range queries are the only requests this app makes in bulk: every panel
// issues one, they all fire on the same refresh tick, and duplicated panels
// (or the same metric on two panels) ask for byte-identical answers. This
// hands all callers within the same tick one in-flight request instead of
// several.
//
// Held only until the request settles — never as a result cache. A stale
// reading on a monitoring dashboard is worse than a second request.
const inFlightRanges = new Map<string, Promise<PrometheusVectorResult>>();

function coalescedRange(path: string): Promise<PrometheusVectorResult> {
  const existing = inFlightRanges.get(path);
  if (existing) return existing;

  const request = apiRequest<PrometheusVectorResult>(path).finally(() => {
    inFlightRanges.delete(path);
  });
  inFlightRanges.set(path, request);
  return request;
}

export function queryRange(promQL: string, start: number, end: number, step: string, datasourceId?: string) {
  const params = new URLSearchParams({
    query: promQL,
    start: String(start),
    end: String(end),
    step,
  });
  if (datasourceId) params.set("datasource", datasourceId);
  return coalescedRange(`/api/v1/query_range?${params}`);
}

export function listAuditLogs() {
  return apiRequest<AuditLog[]>("/api/v1/logs");
}

export function deleteAuditLogs(scope: "all" | "user", userId?: string) {
  return apiRequest<{ deleted: number }>("/api/v1/logs", {
    method: "DELETE",
    body: JSON.stringify({ scope, userId }),
  });
}

export type AlertOperator = ">" | ">=" | "<" | "<=" | "==";

export type AlertRule = {
  id: string;
  name: string;
  promql: string;
  operator: AlertOperator;
  threshold: number;
  channel: string;
  enabled: boolean;
  lastState: "ok" | "firing";
  lastNotifiedAt?: string;
  snoozedUntil?: string;
  ackedAt?: string;
  createdAt: string;
};

export type AlertEvent = {
  id: string;
  ruleId: string;
  ruleName: string;
  event: "fired" | "resolved";
  value: number;
  createdAt: string;
};

export function listAlertRules() {
  return apiRequest<AlertRule[]>("/api/v1/alerts");
}

export function createAlertRule(input: { name: string; promql: string; operator: AlertOperator; threshold: number }) {
  return apiRequest<AlertRule>("/api/v1/alerts", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function setAlertRuleEnabled(id: string, enabled: boolean) {
  return apiRequest<{ status: string }>(`/api/v1/alerts/${id}`, {
    method: "PATCH",
    body: JSON.stringify({ enabled }),
  });
}

export function deleteAlertRule(id: string) {
  return apiRequest<{ status: string }>(`/api/v1/alerts/${id}`, { method: "DELETE" });
}

export function snoozeAlertRule(id: string, minutes: number) {
  return apiRequest<{ status: string }>(`/api/v1/alerts/${id}/snooze`, {
    method: "POST",
    body: JSON.stringify({ minutes }),
  });
}

export function ackAlertRule(id: string) {
  return apiRequest<{ status: string }>(`/api/v1/alerts/${id}/ack`, { method: "POST" });
}

export function getAlertHistory(id: string) {
  return apiRequest<AlertEvent[]>(`/api/v1/alerts/${id}/history`);
}

export type AppRole = "viewer" | "editor" | "admin" | "super_admin";

export type AppUser = {
  id: string;
  email: string;
  role: AppRole;
  createdAt: string;
};

export function listUsers() {
  return apiRequest<AppUser[]>("/api/v1/users");
}

export function createUser(input: { email: string; password: string; role: AppRole }) {
  return apiRequest<{ id: string; email: string; role: AppRole }>("/api/v1/users", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteUser(id: string) {
  return apiRequest<{ status: string }>(`/api/v1/users/${id}`, { method: "DELETE" });
}

export type PanelType = "line" | "area" | "bar" | "pie" | "stat" | "gauge";

export type PanelLayout = {
  id: string;
  title: string;
  promql: string;
  color: string;
  x: number;
  y: number;
  w: number;
  h: number;
  panelType?: PanelType; // undefined == "line", for backward compatibility with saved dashboards
  threshold?: number; // only meaningful for "line" panels — draws a reference line
};

export type DashboardVariable = {
  name: string; // used in panel PromQL as $name
  label: string; // Prometheus label whose values populate the dropdown
};

export type DashboardAnnotation = {
  id: string;
  label: string;
  at: number; // unix seconds
};

export type DashboardDefinition = {
  panels: PanelLayout[];
  variables: DashboardVariable[];
  annotations: DashboardAnnotation[];
};

export type DashboardResult = {
  id: string;
  title: string;
  definition: DashboardDefinition;
  updatedAt: string;
};

// Older saved dashboards stored `definition` as a bare PanelLayout[] (no
// variables/annotations support yet); this normalizes all shapes to the
// current one.
function normalizeDefinition(raw: unknown): DashboardDefinition {
  if (Array.isArray(raw)) return { panels: raw as PanelLayout[], variables: [], annotations: [] };
  const obj = raw as Partial<DashboardDefinition>;
  return { panels: obj.panels ?? [], variables: obj.variables ?? [], annotations: obj.annotations ?? [] };
}

export type DashboardSummary = { id: string; title: string; updatedAt: string };

export function listDashboards() {
  return apiRequest<DashboardSummary[]>("/api/v1/dashboards");
}

export function deleteDashboard(id: string) {
  return apiRequest<{ status: string }>(`/api/v1/dashboards/${id}`, { method: "DELETE" });
}

export async function getDashboard(id: string): Promise<DashboardResult | null> {
  try {
    const res = await apiRequest<{ id: string; title: string; definition: unknown; updatedAt: string }>(
      `/api/v1/dashboards/${id}`,
    );
    return { ...res, definition: normalizeDefinition(res.definition) };
  } catch (err) {
    if ((err as Error).message.includes("bulunamadi")) return null;
    throw err;
  }
}

export function saveDashboard(id: string, title: string, definition: DashboardDefinition) {
  return apiRequest<{ status: string }>(`/api/v1/dashboards/${id}`, {
    method: "PUT",
    body: JSON.stringify({ title, definition }),
  });
}

export function getLabelValues(label: string, datasourceId?: string) {
  const params = datasourceId ? `?${new URLSearchParams({ datasource: datasourceId })}` : "";
  return apiRequest<string[]>(`/api/v1/labels/${encodeURIComponent(label)}/values${params}`);
}

export type Datasource = {
  id: string;
  name: string;
  url: string;
  type: string;
  createdAt: string;
};

export function listDatasources() {
  return apiRequest<Datasource[]>("/api/v1/datasources");
}

export function createDatasource(input: { name: string; url: string; type?: string }) {
  return apiRequest<Datasource>("/api/v1/datasources", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteDatasource(id: string) {
  return apiRequest<{ status: string }>(`/api/v1/datasources/${id}`, { method: "DELETE" });
}

export type MonitorType = "http" | "tcp";

export type Monitor = {
  id: string;
  name: string;
  type: MonitorType;
  target: string;
  intervalSeconds: number;
  lastOk?: boolean;
  lastCheckedAt?: string;
  lastLatencyMs?: number;
  uptimePercent?: number;
  certExpiresAt?: string;
  /** Set while a maintenance window is active — down notifications are held back until this time. */
  snoozedUntil?: string;
  createdAt: string;

  // Custom HTTP check config — http monitors only. Empty on tcp monitors
  // and on any monitor created before this existed.
  method?: string;
  requestBody?: string;
  contentType?: string;
  expectedStatus?: string;
  expectBodyContains?: string;
};

// Uptime across the standard windows a status page reports. A missing
// field means the monitor has no history that far back yet, not 0%.
export type UptimeSummary = {
  day24h?: number;
  day7?: number;
  day30?: number;
  day90?: number;
};

// One calendar day of check results — the unit the 90-day history strip is
// built from. total 0 means no checks ran that day, not that it was down.
export type DailyUptime = {
  date: string;
  total: number;
  failed: number;
  percent: number;
};

// The classes a failed check can carry. These come from what the network
// stack reported, so the set is closed and worth typing: the UI maps each
// one to a list of things worth checking, and a class with no mapping would
// silently render nothing.
export type MonitorFailureClass =
  | "dns"
  | "refused"
  | "timeout_connect"
  | "timeout_response"
  | "tls_cert"
  | "tls_handshake"
  | "http_server"
  | "http_blocked"
  | "http_status"
  | "content_mismatch"
  | "unknown";

export type MonitorCheck = {
  id: string;
  ok: boolean;
  /** How many tries this check needed. Above one means the service wobbled. */
  attempts: number;
  latencyMs: number;
  errorClass?: MonitorFailureClass;
  errorDetail?: string;
  connectMs?: number;
  tlsMs?: number;
  /** The raw HTTP status observed, when there was one — absent for TCP checks and pre-response failures. */
  statusCode?: number;
  checkedAt: string;
};

// One half-hour step of the last day. Counts rather than a verdict: the
// page decides what amber means, and can change that without the server
// having stored the wrong thing.
export type MonitorTimelineBucket = {
  start: string;
  total: number;
  failed: number;
  /** Checks that only passed on a retry — a stall the chat stays quiet about. */
  retried: number;
  worstMs: number;
  medianMs: number;
};

export type MonitorDetail = Monitor & {
  checks: MonitorCheck[];
  timeline: MonitorTimelineBucket[];
  uptimeSummary: UptimeSummary;
  dailyHistory: DailyUptime[];
};

export function getMonitorDetail(id: string) {
  return apiRequest<MonitorDetail>(`/api/v1/monitors/${id}/checks`);
}

// minutes <= 0 clears an active snooze immediately.
export function snoozeMonitor(id: string, minutes: number) {
  return apiRequest<{ status: string }>(`/api/v1/monitors/${id}/snooze`, {
    method: "POST",
    body: JSON.stringify({ minutes }),
  });
}

export function listMonitors() {
  return apiRequest<Monitor[]>("/api/v1/monitors");
}

export function createMonitor(input: {
  name: string;
  type: MonitorType;
  target: string;
  intervalSeconds: number;
  method?: string;
  requestBody?: string;
  contentType?: string;
  expectedStatus?: string;
  expectBodyContains?: string;
}) {
  return apiRequest<Monitor>("/api/v1/monitors", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteMonitor(id: string) {
  return apiRequest<{ status: string }>(`/api/v1/monitors/${id}`, { method: "DELETE" });
}

export type StatusPageMonitor = {
  name: string;
  ok?: boolean;
  uptimePercent?: number;
  uptimeSummary: UptimeSummary;
  dailyHistory: DailyUptime[];
};

// Unauthenticated on purpose — this is the public status page endpoint.
// apiRequest still works fine here: it just won't attach a token (or will
// harmlessly attach one if the visitor happens to be logged in too).
export function getStatusPage() {
  return apiRequest<StatusPageMonitor[]>("/api/v1/status-page");
}

export type Settings = Record<string, string>;

export function getSettings() {
  return apiRequest<Settings>("/api/v1/settings");
}

export function updateSetting(key: string, value: boolean) {
  return updateSettingValue(key, value ? "true" : "false");
}

// For settings that aren't a toggle — the outgoing webhook URL is the only
// one today.
export function updateSettingValue(key: string, value: string) {
  return apiRequest<{ status: string }>(`/api/v1/settings/${key}`, {
    method: "PUT",
    body: JSON.stringify({ value }),
  });
}

// --- Browser push notifications -------------------------------------------

export function getPushVapidKey() {
  return apiRequest<{ publicKey: string }>("/api/v1/push/vapid-key");
}

export function subscribePush(subscription: PushSubscriptionJSON) {
  return apiRequest<{ status: string }>("/api/v1/push/subscribe", {
    method: "POST",
    body: JSON.stringify({
      endpoint: subscription.endpoint,
      keys: { p256dh: subscription.keys?.p256dh ?? "", auth: subscription.keys?.auth ?? "" },
    }),
  });
}

export function unsubscribePush(endpoint: string) {
  return apiRequest<{ status: string }>("/api/v1/push/unsubscribe", {
    method: "POST",
    body: JSON.stringify({ endpoint }),
  });
}

// --- Auto-discovery -------------------------------------------------------
// Reports which exporters the connected Prometheus is scraping, so a fresh
// install can propose real panels instead of an empty dashboard and a
// PromQL prompt.

export type DiscoveredPanel = {
  titleTr: string;
  titleEn: string;
  promql: string;
  unit: "percent" | "bytes" | "count" | "rate";
};

export type DiscoveredIntegration = {
  id: string;
  nameTr: string;
  nameEn: string;
  panels: DiscoveredPanel[];
};

export type DiscoveryResult = {
  prometheusReachable: boolean;
  metricCount: number;
  integrations: DiscoveredIntegration[];
};

export function discoverIntegrations(datasourceId?: string) {
  const params = datasourceId ? `?${new URLSearchParams({ datasource: datasourceId })}` : "";
  return apiRequest<DiscoveryResult>(`/api/v1/discover${params}`);
}

// --- First-run setup ------------------------------------------------------
// Both endpoints are unauthenticated by necessity — there is no account to
// authenticate as until setup has run — and the server refuses them once
// one exists.

export function getSetupStatus() {
  return apiRequest<{ needsSetup: boolean }>("/api/v1/setup");
}

export function completeSetup(email: string, password: string) {
  return apiRequest<{ status: string }>("/api/v1/setup", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });
}

// --- Dashboard share links ------------------------------------------------
// The token in the URL is the whole capability; the endpoints below are
// unauthenticated and can only run the shared dashboard's own queries.

export function getShareToken(dashboardId: string) {
  return apiRequest<{ token: string }>(`/api/v1/dashboards/${dashboardId}/share`);
}

export function createShareLink(dashboardId: string) {
  return apiRequest<{ token: string }>(`/api/v1/dashboards/${dashboardId}/share`, { method: "POST" });
}

export function revokeShareLink(dashboardId: string) {
  return apiRequest<{ status: string }>(`/api/v1/dashboards/${dashboardId}/share`, { method: "DELETE" });
}

export function getSharedDashboard(token: string) {
  return apiRequest<{ title: string; definition: DashboardDefinition }>(
    `/api/v1/shared/${encodeURIComponent(token)}`,
  );
}

export function sharedQueryRange(token: string, promQL: string, start: number, end: number, step: string) {
  const params = new URLSearchParams({ query: promQL, start: String(start), end: String(end), step });
  return coalescedRange(`/api/v1/shared/${encodeURIComponent(token)}/query_range?${params}`);
}

// Traffic analytics. Unlike everything above, none of this comes from
// Prometheus: it is read from the web server's own access log, because
// per-request facts (which IP, which path, which client) are not something
// any exporter publishes. Admin+ only, since it contains visitors' IP
// addresses and the URLs they asked for.
export type TrafficRange = "1h" | "6h" | "24h" | "7d";

export type TrafficDim = { key: string; requests: number; bytes: number; errors: number };

export type TrafficSource = {
  id: string;
  name: string;
  path: string;
  lines: number;
  unparsed: number;
  lastReadAt?: string;
};

export type TrafficPoint = {
  at: number; // unix seconds
  requests: number;
  bytes: number;
  s2xx: number;
  s3xx: number;
  s4xx: number;
  s5xx: number;
};

export type TrafficRequestRow = {
  id: number;
  at: string;
  source: string;
  ip: string;
  host?: string;
  method: string;
  path: string;
  status: number;
  bytes: number;
  userAgent?: string;
  service?: string;
  durationMs?: number;
};

export type TrafficOverview = {
  // enabled: a log path is configured or was auto-detected.
  // hasData: something has actually been read from it. The two are
  // different problems with different fixes, so the page is told both.
  enabled: boolean;
  hasData: boolean;
  configuredPaths: string[];
  sources: TrafficSource[];
  // Which panels this log format can fill. False means the field is absent
  // from log_format, not that nothing happened.
  fields: { host: boolean; service: boolean; duration: boolean };
  range: TrafficRange;
  stepSeconds: number;
  totals: {
    requests: number;
    bytes: number;
    errors5xx: number;
    errors4xx: number;
    unauthorized: number;
    requestsPerSecond: number;
    bytesPerSecond: number;
    peakRequestsPerSecond: number;
    errorRate: number;
    avgDurationMs: number;
    hasDuration: boolean;
  };
  series: TrafficPoint[];
  topIps: TrafficDim[];
  topIpsByBytes: TrafficDim[];
  topPaths: TrafficDim[];
  topClients: TrafficDim[];
  topHosts: TrafficDim[];
  topServices: TrafficDim[];
  nodes: TrafficDim[];
  statusCodes: TrafficDim[];
  methods: TrafficDim[];
  errorPaths: TrafficDim[];
  unauthorized: TrafficRequestRow[];
  recent: TrafficRequestRow[];
};

export function getTraffic(range: TrafficRange) {
  return apiRequest<TrafficOverview>(`/api/v1/traffic?range=${range}`);
}

export function getTrafficRequests(filter: "all" | "unauthorized" | "errors", range: TrafficRange) {
  return apiRequest<TrafficRequestRow[]>(`/api/v1/traffic/requests?filter=${filter}&range=${range}`);
}
