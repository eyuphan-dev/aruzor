import { defaultQueries } from "./defaultQueries";

type Localized = { tr: string; en: string };

export type PanelTemplate = {
  title: Localized;
  promql: string;
  color: string;
  w: number;
  h: number;
};

export type DashboardTemplate = {
  id: string;
  name: Localized;
  description: Localized;
  panels: PanelTemplate[];
};

export const dashboardTemplates: DashboardTemplate[] = [
  {
    id: "node-exporter",
    name: { tr: "Node Exporter (Sunucu Sağlığı)", en: "Node Exporter (Server Health)" },
    description: {
      tr: "CPU, bellek, disk, ağ ve sistem yükü — temel sunucu izleme paneli.",
      en: "CPU, memory, disk, network and system load — basic server monitoring panels.",
    },
    panels: [
      { title: { tr: "İşlemci Kullanımı", en: "CPU Usage" }, promql: defaultQueries.cpu, color: "#02c39a", w: 6, h: 4 },
      { title: { tr: "Bellek Kullanımı", en: "Memory Usage" }, promql: defaultQueries.memory, color: "#00a896", w: 6, h: 4 },
      { title: { tr: "Disk Kullanımı", en: "Disk Usage" }, promql: defaultQueries.disk, color: "#f59e0b", w: 6, h: 4 },
      { title: { tr: "Ağ Trafiği", en: "Network Traffic" }, promql: defaultQueries.network, color: "#10b981", w: 6, h: 4 },
      { title: { tr: "Sistem Yükü (1dk)", en: "System Load (1m)" }, promql: "node_load1", color: "#02c39a", w: 6, h: 4 },
      {
        title: { tr: "Disk I/O", en: "Disk I/O" },
        promql: "sum(rate(node_disk_io_time_seconds_total[5m]))",
        color: "#00a896",
        w: 6,
        h: 4,
      },
    ],
  },
  {
    id: "postgres-exporter",
    name: { tr: "Postgres Exporter", en: "Postgres Exporter" },
    description: {
      tr: "PostgreSQL bağlantı sayısı, veritabanı boyutu ve işlem hızı.",
      en: "PostgreSQL connection count, database size and transaction rate.",
    },
    panels: [
      { title: { tr: "Aktif Bağlantılar", en: "Active Connections" }, promql: "sum(pg_stat_activity_count)", color: "#02c39a", w: 6, h: 4 },
      {
        title: { tr: "Veritabanı Boyutu (byte)", en: "Database Size (bytes)" },
        promql: "sum(pg_database_size_bytes)",
        color: "#00a896",
        w: 6,
        h: 4,
      },
      {
        title: { tr: "İşlem Hızı (commit/sn)", en: "Transaction Rate (commits/s)" },
        promql: "sum(rate(pg_stat_database_xact_commit_total[5m]))",
        color: "#f59e0b",
        w: 6,
        h: 4,
      },
      {
        title: { tr: "Cache Hit Oranı", en: "Cache Hit Ratio" },
        promql:
          "sum(rate(pg_stat_database_blks_hit_total[5m])) / (sum(rate(pg_stat_database_blks_hit_total[5m])) + sum(rate(pg_stat_database_blks_read_total[5m])))",
        color: "#10b981",
        w: 6,
        h: 4,
      },
    ],
  },
  {
    id: "kube-state-metrics",
    name: { tr: "Kube-State-Metrics", en: "Kube-State-Metrics" },
    description: {
      tr: "Kubernetes pod, node ve deployment sağlığı.",
      en: "Kubernetes pod, node and deployment health.",
    },
    panels: [
      {
        title: { tr: "Çalışan Pod Sayısı", en: "Running Pods" },
        promql: 'count(kube_pod_status_phase{phase="Running"})',
        color: "#02c39a",
        w: 6,
        h: 4,
      },
      {
        title: { tr: "Pod Yeniden Başlatmaları", en: "Pod Restarts" },
        promql: "sum(rate(kube_pod_container_status_restarts_total[15m]))",
        color: "#f59e0b",
        w: 6,
        h: 4,
      },
      {
        title: { tr: "Hazır Node Sayısı", en: "Ready Nodes" },
        promql: 'sum(kube_node_status_condition{condition="Ready",status="true"})',
        color: "#00a896",
        w: 6,
        h: 4,
      },
      {
        title: { tr: "Deployment Kullanılabilir Replika", en: "Deployment Available Replicas" },
        promql: "sum(kube_deployment_status_replicas_available)",
        color: "#10b981",
        w: 6,
        h: 4,
      },
    ],
  },
];
