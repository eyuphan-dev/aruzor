// Short human-readable explanations for the most common exporter metrics,
// shown next to the raw Prometheus metric name so users don't have to
// guess what e.g. "node_memory_MemAvailable_bytes" means.
const descriptions: Record<string, { tr: string; en: string }> = {
  node_cpu_seconds_total: {
    tr: "İşlemcinin modlara (boşta, kullanıcı, sistem vb.) göre harcadığı toplam saniye — genelde kullanım yüzdesi hesaplamak için oran alınır.",
    en: "Total CPU seconds spent per mode (idle, user, system, etc.) — usually rated to compute a usage percentage.",
  },
  node_memory_MemAvailable_bytes: {
    tr: "Yeni işlemler için kullanılabilir tahmini bellek miktarı (bayt).",
    en: "Estimated memory available for new processes (bytes).",
  },
  node_memory_MemTotal_bytes: {
    tr: "Sunucudaki toplam fiziksel bellek (bayt).",
    en: "Total physical memory on the server (bytes).",
  },
  node_filesystem_avail_bytes: {
    tr: "Dosya sisteminde kullanıcılar için boş kalan alan (bayt).",
    en: "Free disk space available to non-root users (bytes).",
  },
  node_filesystem_size_bytes: {
    tr: "Dosya sisteminin toplam boyutu (bayt).",
    en: "Total size of the filesystem (bytes).",
  },
  node_load1: {
    tr: "Son 1 dakikalık ortalama sistem yükü (çalışan/bekleyen işlem sayısı).",
    en: "1-minute load average (running/waiting process count).",
  },
  node_load5: {
    tr: "Son 5 dakikalık ortalama sistem yükü.",
    en: "5-minute load average.",
  },
  node_load15: {
    tr: "Son 15 dakikalık ortalama sistem yükü.",
    en: "15-minute load average.",
  },
  node_network_receive_bytes_total: {
    tr: "Ağ arayüzünden alınan toplam bayt sayısı (kümülatif sayaç).",
    en: "Total bytes received on a network interface (cumulative counter).",
  },
  node_network_transmit_bytes_total: {
    tr: "Ağ arayüzünden gönderilen toplam bayt sayısı (kümülatif sayaç).",
    en: "Total bytes transmitted on a network interface (cumulative counter).",
  },
  node_disk_io_time_seconds_total: {
    tr: "Diskin G/Ç işlemleriyle meşgul olduğu toplam süre (saniye) — disk darboğazını gösterir.",
    en: "Total time the disk spent doing I/O (seconds) — indicates disk bottlenecks.",
  },
  node_disk_read_bytes_total: {
    tr: "Diskten okunan toplam bayt sayısı (kümülatif sayaç).",
    en: "Total bytes read from disk (cumulative counter).",
  },
  node_disk_written_bytes_total: {
    tr: "Diske yazılan toplam bayt sayısı (kümülatif sayaç).",
    en: "Total bytes written to disk (cumulative counter).",
  },
  up: {
    tr: "Prometheus'un hedefi başarıyla tarayabildiğini (1) veya taramanın başarısız olduğunu (0) gösterir.",
    en: "Whether Prometheus could successfully scrape the target (1) or the scrape failed (0).",
  },
  pg_stat_activity_count: {
    tr: "PostgreSQL'e o an açık olan bağlantı/oturum sayısı.",
    en: "Number of currently open PostgreSQL connections/sessions.",
  },
  pg_database_size_bytes: {
    tr: "Veritabanının disk üzerindeki boyutu (bayt).",
    en: "Size of the database on disk (bytes).",
  },
  process_resident_memory_bytes: {
    tr: "Sürecin kullandığı fiziksel bellek miktarı (bayt).",
    en: "Physical memory used by the process (bytes).",
  },
  process_cpu_seconds_total: {
    tr: "Sürecin başlangıcından beri harcadığı toplam CPU süresi (saniye).",
    en: "Total CPU time consumed by the process since start (seconds).",
  },
  go_goroutines: {
    tr: "O anda çalışan Go goroutine sayısı — ani artışlar sızıntı/tıkanıklık işareti olabilir.",
    en: "Number of currently running Go goroutines — sudden spikes can indicate leaks or contention.",
  },
  go_memstats_alloc_bytes: {
    tr: "Go çalışma zamanının şu an ayırdığı ve kullandığı bellek (bayt).",
    en: "Memory currently allocated and in use by the Go runtime (bytes).",
  },
};

// Prefixes that hint at what an unmapped metric measures, used as a
// fallback when there's no exact description above.
const prefixHints: [string, { tr: string; en: string }][] = [
  ["node_cpu", { tr: "İşlemci ile ilgili bir metrik.", en: "A CPU-related metric." }],
  ["node_memory", { tr: "Bellek ile ilgili bir metrik.", en: "A memory-related metric." }],
  ["node_filesystem", { tr: "Disk/dosya sistemi ile ilgili bir metrik.", en: "A disk/filesystem-related metric." }],
  ["node_disk", { tr: "Disk G/Ç ile ilgili bir metrik.", en: "A disk I/O-related metric." }],
  ["node_network", { tr: "Ağ trafiği ile ilgili bir metrik.", en: "A network traffic-related metric." }],
  ["pg_", { tr: "PostgreSQL veritabanı ile ilgili bir metrik.", en: "A PostgreSQL database-related metric." }],
  ["kube_", { tr: "Kubernetes ile ilgili bir metrik.", en: "A Kubernetes-related metric." }],
  ["go_", { tr: "Go çalışma zamanı (runtime) ile ilgili bir metrik.", en: "A Go runtime-related metric." }],
  ["process_", { tr: "Bir sürecin (process) kaynak kullanımıyla ilgili bir metrik.", en: "A process resource-usage metric." }],
];

export function describeMetric(name: string, locale: "tr" | "en"): string | null {
  const exact = descriptions[name];
  if (exact) return exact[locale];

  const hint = prefixHints.find(([prefix]) => name.startsWith(prefix));
  if (hint) return hint[1][locale];

  return null;
}
