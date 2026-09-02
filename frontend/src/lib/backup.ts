import {
  createAlertRule,
  createDatasource,
  createMonitor,
  getDashboard,
  listAlertRules,
  listDashboards,
  listDatasources,
  listMonitors,
  saveDashboard,
  type AlertOperator,
  type DashboardDefinition,
  type MonitorType,
} from "./api";
import { genId } from "./id";

// Whole-instance backup. Dashboards could already be exported one at a
// time, which is the wrong granularity for the thing people actually want
// to protect: moving to a new server, or getting back after a mistake,
// means alert rules and monitors too.
//
// Built entirely on the existing REST endpoints rather than a new "dump the
// database" route. That keeps the file readable and — more importantly —
// keeps every write going through the same role checks as the UI, so a
// restore can never grant more than the person running it already has.

export const BACKUP_FORMAT = "aruzor-backup";
export const BACKUP_VERSION = 1;

type BackupDashboard = { id: string; title: string; definition: DashboardDefinition };
type BackupAlert = { name: string; promql: string; operator: AlertOperator; threshold: number };
// Mirrors MonitorInput's optional custom-HTTP-check fields exactly. Left
// off a monitor that never used them (undefined, not written to the file at
// all — see createBackup below), so a backup made before that feature
// existed and one made after it but unused both restore identically.
type BackupMonitor = {
  name: string;
  type: MonitorType;
  target: string;
  intervalSeconds: number;
  method?: string;
  requestBody?: string;
  contentType?: string;
  expectedStatus?: string;
  expectBodyContains?: string;
};
type BackupDatasource = { name: string; url: string; type: string };

export type Backup = {
  format: typeof BACKUP_FORMAT;
  version: number;
  exportedAt: string;
  dashboards: BackupDashboard[];
  alerts: BackupAlert[];
  monitors: BackupMonitor[];
  datasources: BackupDatasource[];
};

export type RestoreReport = {
  dashboards: { created: number; skipped: number };
  alerts: { created: number; skipped: number };
  monitors: { created: number; skipped: number };
  datasources: { created: number; skipped: number };
  errors: string[];
};

// Runtime state — last fired, current uptime, latest latency — is
// deliberately left out. It describes what the old server observed, not how
// this one is configured, and restoring it would present a fiction as fact.
export async function createBackup(): Promise<Backup> {
  const [summaries, alerts, monitors, datasources] = await Promise.all([
    listDashboards(),
    listAlertRules(),
    listMonitors(),
    listDatasources(),
  ]);

  const dashboards: BackupDashboard[] = [];
  for (const summary of summaries) {
    const full = await getDashboard(summary.id);
    if (full) dashboards.push({ id: summary.id, title: full.title, definition: full.definition });
  }

  return {
    format: BACKUP_FORMAT,
    version: BACKUP_VERSION,
    exportedAt: new Date().toISOString(),
    dashboards,
    alerts: alerts.map((a) => ({
      name: a.name,
      promql: a.promql,
      operator: a.operator,
      threshold: a.threshold,
    })),
    monitors: monitors.map((m) => ({
      name: m.name,
      type: m.type,
      target: m.target,
      intervalSeconds: m.intervalSeconds,
      // Custom HTTP check fields (method/body/expected status/expected
      // content) — omitted rather than written as empty strings, so a
      // restored monitor that never used them looks exactly like one
      // created fresh, not like one with a check nobody configured.
      method: m.method || undefined,
      requestBody: m.requestBody || undefined,
      contentType: m.contentType || undefined,
      expectedStatus: m.expectedStatus || undefined,
      expectBodyContains: m.expectBodyContains || undefined,
    })),
    // The "default" datasource is recreated by the server on first boot and
    // points at whatever Prometheus that machine found. Carrying the old
    // machine's address over would break the restore rather than complete it.
    datasources: datasources
      .filter((d) => d.id !== "default")
      .map((d) => ({ name: d.name, url: d.url, type: d.type })),
  };
}

export function parseBackup(text: string): Backup {
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    throw new Error("invalid-json");
  }
  const candidate = parsed as Partial<Backup>;
  if (candidate?.format !== BACKUP_FORMAT) throw new Error("not-a-backup");
  if (typeof candidate.version !== "number" || candidate.version > BACKUP_VERSION) {
    throw new Error("unsupported-version");
  }
  return {
    ...candidate,
    dashboards: candidate.dashboards ?? [],
    alerts: candidate.alerts ?? [],
    monitors: candidate.monitors ?? [],
    datasources: candidate.datasources ?? [],
  } as Backup;
}

// Restore is additive and never overwrites. Anything whose name already
// exists is skipped and counted — a restore run twice, or run onto a server
// that is already partly set up, should not silently replace work that is
// there. Failures are collected instead of aborting: one bad alert rule
// should not cost the user the other forty items in the file.
export async function restoreBackup(backup: Backup): Promise<RestoreReport> {
  const report: RestoreReport = {
    dashboards: { created: 0, skipped: 0 },
    alerts: { created: 0, skipped: 0 },
    monitors: { created: 0, skipped: 0 },
    datasources: { created: 0, skipped: 0 },
    errors: [],
  };

  const [existingDashboards, existingAlerts, existingMonitors, existingDatasources] = await Promise.all([
    listDashboards(),
    listAlertRules(),
    listMonitors(),
    listDatasources(),
  ]);

  const dashboardTitles = new Set(existingDashboards.map((d) => d.title));
  for (const d of backup.dashboards) {
    if (dashboardTitles.has(d.title)) {
      report.dashboards.skipped++;
      continue;
    }
    try {
      // A fresh id: reusing the exported one would collide with an unrelated
      // dashboard that happens to share it on this server.
      await saveDashboard(genId(), d.title, d.definition);
      report.dashboards.created++;
      dashboardTitles.add(d.title);
    } catch (err) {
      report.errors.push(`${d.title}: ${(err as Error).message}`);
    }
  }

  const alertNames = new Set(existingAlerts.map((a) => a.name));
  for (const a of backup.alerts) {
    if (alertNames.has(a.name)) {
      report.alerts.skipped++;
      continue;
    }
    try {
      await createAlertRule({ name: a.name, promql: a.promql, operator: a.operator, threshold: a.threshold });
      report.alerts.created++;
      alertNames.add(a.name);
    } catch (err) {
      report.errors.push(`${a.name}: ${(err as Error).message}`);
    }
  }

  const monitorNames = new Set(existingMonitors.map((m) => m.name));
  for (const m of backup.monitors) {
    if (monitorNames.has(m.name)) {
      report.monitors.skipped++;
      continue;
    }
    try {
      await createMonitor({
        name: m.name,
        type: m.type,
        target: m.target,
        intervalSeconds: m.intervalSeconds,
        method: m.method,
        requestBody: m.requestBody,
        contentType: m.contentType,
        expectedStatus: m.expectedStatus,
        expectBodyContains: m.expectBodyContains,
      });
      report.monitors.created++;
      monitorNames.add(m.name);
    } catch (err) {
      report.errors.push(`${m.name}: ${(err as Error).message}`);
    }
  }

  const datasourceNames = new Set(existingDatasources.map((d) => d.name));
  for (const d of backup.datasources) {
    if (datasourceNames.has(d.name)) {
      report.datasources.skipped++;
      continue;
    }
    try {
      await createDatasource({ name: d.name, url: d.url, type: d.type });
      report.datasources.created++;
      datasourceNames.add(d.name);
    } catch (err) {
      report.errors.push(`${d.name}: ${(err as Error).message}`);
    }
  }

  return report;
}
