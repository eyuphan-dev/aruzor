export const defaultQueries = {
  cpu: `100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`,
  memory: `(1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)) * 100`,
  disk: `(1 - (node_filesystem_avail_bytes{fstype!="tmpfs"} / node_filesystem_size_bytes{fstype!="tmpfs"})) * 100`,
  network: `sum(rate(node_network_receive_bytes_total{device!="lo"}[5m]))`,
} as const;
