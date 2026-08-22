// Package discovery works out which exporters a Prometheus is actually
// scraping, so Aruzor can populate itself on a server it has never seen
// before instead of presenting an empty dashboard and a PromQL prompt.
//
// The signal is the metric namespace: every mainstream exporter publishes
// metrics under a prefix that no other exporter uses. Asking Prometheus
// for its metric names once and matching prefixes is both cheap and
// independent of how the targets were configured — file_sd, static
// configs and Kubernetes service discovery all look the same from here.
package discovery

import (
	"context"
	"encoding/json"
	"strings"

	"aruzor/internal/prometheus"
)

// Panel is a suggested chart: a title in both languages and the query
// behind it. Kept deliberately close to the frontend's panel shape so a
// suggestion can become a real panel without translation.
type Panel struct {
	TitleTR string `json:"titleTr"`
	TitleEN string `json:"titleEn"`
	PromQL  string `json:"promql"`
	Unit    string `json:"unit"` // "percent" | "bytes" | "count" | "rate"
}

// Integration is one detected exporter and the panels worth showing for it.
type Integration struct {
	ID     string  `json:"id"`
	NameTR string  `json:"nameTr"`
	NameEN string  `json:"nameEn"`
	Panels []Panel `json:"panels"`
}

// Result is what the API returns: what was found, plus enough context for
// the UI to explain an empty answer.
type Result struct {
	PrometheusReachable bool          `json:"prometheusReachable"`
	MetricCount         int           `json:"metricCount"`
	Integrations        []Integration `json:"integrations"`
}

type catalogEntry struct {
	integration Integration
	// prefixes: presence of any one of these means the exporter is scraped
	prefixes []string
}

// catalog maps a metric namespace to the panels that namespace makes
// possible. Ordered most-general first so a generated dashboard opens with
// host health before drilling into individual services.
var catalog = []catalogEntry{
	{
		integration: Integration{
			ID: "node", NameTR: "Sunucu (Node Exporter)", NameEN: "Server (Node Exporter)",
			Panels: []Panel{
				{"İşlemci Kullanımı", "CPU Usage", `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`, "percent"},
				{"Bellek Kullanımı", "Memory Usage", `(1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)) * 100`, "percent"},
				{"Disk Kullanımı", "Disk Usage", `(1 - (node_filesystem_avail_bytes{fstype!="tmpfs"} / node_filesystem_size_bytes{fstype!="tmpfs"})) * 100`, "percent"},
				{"Ağ Trafiği", "Network Traffic", `sum by (instance) (rate(node_network_receive_bytes_total{device!="lo"}[5m]))`, "rate"},
				{"Sistem Yükü", "Load Average", `sum by (instance) (node_load1)`, "count"},
				{"Disk G/Ç", "Disk I/O", `sum by (instance) (rate(node_disk_read_bytes_total[5m]) + rate(node_disk_written_bytes_total[5m]))`, "rate"},
			},
		},
		prefixes: []string{"node_cpu_seconds_total", "node_memory_MemTotal_bytes"},
	},
	{
		integration: Integration{
			ID: "windows", NameTR: "Windows Sunucu", NameEN: "Windows Server",
			Panels: []Panel{
				{"İşlemci Kullanımı", "CPU Usage", `100 - (avg by (instance) (rate(windows_cpu_time_total{mode="idle"}[5m])) * 100)`, "percent"},
				{"Bellek Kullanımı", "Memory Usage", `(1 - (windows_os_physical_memory_free_bytes / windows_cs_physical_memory_bytes)) * 100`, "percent"},
				{"Disk Kullanımı", "Disk Usage", `(1 - (windows_logical_disk_free_bytes / windows_logical_disk_size_bytes)) * 100`, "percent"},
			},
		},
		prefixes: []string{"windows_cpu_time_total", "windows_os_info"},
	},
	{
		integration: Integration{
			ID: "container", NameTR: "Konteynerler (cAdvisor)", NameEN: "Containers (cAdvisor)",
			Panels: []Panel{
				{"Konteyner İşlemci", "Container CPU", `sum(rate(container_cpu_usage_seconds_total{name!=""}[5m])) by (name) * 100`, "percent"},
				{"Konteyner Bellek", "Container Memory", `sum(container_memory_usage_bytes{name!=""}) by (name)`, "bytes"},
				{"Konteyner Ağ", "Container Network", `sum(rate(container_network_receive_bytes_total{name!=""}[5m])) by (name)`, "rate"},
			},
		},
		prefixes: []string{"container_cpu_usage_seconds_total"},
	},
	{
		integration: Integration{
			ID: "postgres", NameTR: "PostgreSQL", NameEN: "PostgreSQL",
			Panels: []Panel{
				{"Aktif Bağlantılar", "Active Connections", `sum(pg_stat_activity_count)`, "count"},
				{"İşlem Hızı", "Transaction Rate", `sum(rate(pg_stat_database_xact_commit[5m]))`, "rate"},
				{"Cache İsabet Oranı", "Cache Hit Ratio", `sum(pg_stat_database_blks_hit) / (sum(pg_stat_database_blks_hit) + sum(pg_stat_database_blks_read)) * 100`, "percent"},
				{"Veritabanı Boyutu", "Database Size", `sum(pg_database_size_bytes)`, "bytes"},
			},
		},
		prefixes: []string{"pg_stat_database_xact_commit", "pg_up"},
	},
	{
		integration: Integration{
			ID: "mysql", NameTR: "MySQL / MariaDB", NameEN: "MySQL / MariaDB",
			Panels: []Panel{
				{"Aktif Bağlantılar", "Active Connections", `sum(mysql_global_status_threads_connected)`, "count"},
				{"Sorgu Hızı", "Query Rate", `sum(rate(mysql_global_status_queries[5m]))`, "rate"},
				{"Yavaş Sorgular", "Slow Queries", `sum(rate(mysql_global_status_slow_queries[5m]))`, "rate"},
			},
		},
		prefixes: []string{"mysql_global_status_queries", "mysql_up"},
	},
	{
		integration: Integration{
			ID: "redis", NameTR: "Redis", NameEN: "Redis",
			Panels: []Panel{
				{"Bağlı İstemciler", "Connected Clients", `sum(redis_connected_clients)`, "count"},
				{"Bellek Kullanımı", "Memory Used", `sum(redis_memory_used_bytes)`, "bytes"},
				{"Komut Hızı", "Command Rate", `sum(rate(redis_commands_processed_total[5m]))`, "rate"},
				{"Cache İsabet Oranı", "Hit Ratio", `sum(rate(redis_keyspace_hits_total[5m])) / (sum(rate(redis_keyspace_hits_total[5m])) + sum(rate(redis_keyspace_misses_total[5m]))) * 100`, "percent"},
			},
		},
		prefixes: []string{"redis_connected_clients", "redis_up"},
	},
	{
		integration: Integration{
			ID: "nginx", NameTR: "Nginx", NameEN: "Nginx",
			Panels: []Panel{
				{"İstek Hızı", "Request Rate", `sum(rate(nginx_http_requests_total[5m]))`, "rate"},
				{"Aktif Bağlantılar", "Active Connections", `sum(nginx_connections_active)`, "count"},
			},
		},
		prefixes: []string{"nginx_http_requests_total", "nginx_connections_active"},
	},
	{
		integration: Integration{
			ID: "blackbox", NameTR: "Erişilebilirlik (Blackbox)", NameEN: "Availability (Blackbox)",
			Panels: []Panel{
				{"Hedef Erişilebilirliği", "Target Availability", `probe_success`, "count"},
				{"Yanıt Süresi", "Response Time", `probe_duration_seconds`, "count"},
				{"SSL Sertifika Ömrü (gün)", "SSL Cert Expiry (days)", `(probe_ssl_earliest_cert_expiry - time()) / 86400`, "count"},
			},
		},
		prefixes: []string{"probe_success"},
	},
	{
		integration: Integration{
			ID: "kubernetes", NameTR: "Kubernetes", NameEN: "Kubernetes",
			Panels: []Panel{
				{"Çalışan Pod Sayısı", "Running Pods", `sum(kube_pod_status_phase{phase="Running"})`, "count"},
				{"Pod Yeniden Başlatmaları", "Pod Restarts", `sum(rate(kube_pod_container_status_restarts_total[15m]))`, "rate"},
				{"Hazır Node Sayısı", "Ready Nodes", `sum(kube_node_status_condition{condition="Ready",status="true"})`, "count"},
			},
		},
		prefixes: []string{"kube_pod_status_phase", "kube_node_info"},
	},
	{
		integration: Integration{
			ID: "prometheus", NameTR: "Prometheus", NameEN: "Prometheus",
			Panels: []Panel{
				{"Hedef Sağlığı", "Target Health", `up`, "count"},
				{"Zaman Serisi Sayısı", "Active Series", `prometheus_tsdb_head_series`, "count"},
				{"Tarama Süresi", "Scrape Duration", `avg(scrape_duration_seconds)`, "count"},
			},
		},
		prefixes: []string{"prometheus_tsdb_head_series"},
	},
}

// Run asks Prometheus what it knows about and matches that against the
// catalog. A Prometheus that is unreachable and one that is reachable but
// empty are reported differently — the first is a setup problem, the
// second just means no exporters are being scraped yet.
func Run(ctx context.Context, prom *prometheus.Client) Result {
	data, err := prom.MetricNames(ctx)
	if err != nil {
		return Result{PrometheusReachable: false, Integrations: []Integration{}}
	}

	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return Result{PrometheusReachable: true, Integrations: []Integration{}}
	}

	// A set lookup keeps this linear in the number of metric names, which on
	// a busy Prometheus runs to tens of thousands.
	present := make(map[string]struct{}, len(names))
	for _, n := range names {
		present[n] = struct{}{}
	}

	result := Result{PrometheusReachable: true, MetricCount: len(names), Integrations: []Integration{}}
	for _, entry := range catalog {
		if !anyPresent(entry.prefixes, present) {
			continue
		}
		integration := entry.integration
		integration.Panels = availablePanels(integration.Panels, present)
		if len(integration.Panels) == 0 {
			continue
		}
		result.Integrations = append(result.Integrations, integration)
	}
	return result
}

func anyPresent(names []string, present map[string]struct{}) bool {
	for _, n := range names {
		if _, ok := present[n]; ok {
			return true
		}
	}
	return false
}

// availablePanels drops any suggestion whose metrics this Prometheus does
// not actually carry. Detecting an exporter by one metric does not mean
// every metric it can publish is enabled — suggesting a panel that would
// render permanently empty is worse than suggesting nothing.
func availablePanels(panels []Panel, present map[string]struct{}) []Panel {
	out := make([]Panel, 0, len(panels))
	for _, p := range panels {
		if hasAllMetrics(p.PromQL, present) {
			out = append(out, p)
		}
	}
	return out
}

func hasAllMetrics(promQL string, present map[string]struct{}) bool {
	for _, name := range metricNamesIn(promQL) {
		if _, ok := present[name]; !ok {
			return false
		}
	}
	return true
}

// groupingKeywords are followed by a parenthesised list of *label* names.
// Those look exactly like metric names to a character-level scanner, so the
// list has to be skipped wholesale rather than filtered afterwards.
var groupingKeywords = map[string]bool{
	"by": true, "without": true, "on": true, "ignoring": true,
	"group_left": true, "group_right": true,
}

// otherKeywords are PromQL words that are never metric names. Aggregation
// operators have to be listed explicitly rather than relying on "followed
// by a parenthesis": in the "sum by (x) (metric)" form the operator is
// followed by the grouping clause instead.
var otherKeywords = map[string]bool{
	"and": true, "or": true, "unless": true,
	"offset": true, "bool": true, "inf": true, "nan": true,
	"sum": true, "min": true, "max": true, "avg": true, "group": true,
	"stddev": true, "stdvar": true, "count": true, "count_values": true,
	"bottomk": true, "topk": true, "quantile": true,
}

// metricNamesIn extracts the bare metric names a query references. The
// parser is intentionally small: it walks the query for identifier-shaped
// runs, skips anything inside a {label="..."} selector or a by(...) label
// list, and ignores any token immediately followed by "(" since that is a
// function name.
func metricNamesIn(promQL string) []string {
	var names []string
	var current strings.Builder

	// flush classifies the token just read. It reports whether that token
	// was a grouping keyword, which tells the caller to skip the label list
	// that follows.
	flush := func(nextIsParen bool) (wasGrouping bool) {
		token := current.String()
		current.Reset()
		if token == "" {
			return false
		}
		if groupingKeywords[token] {
			return true
		}
		if nextIsParen || otherKeywords[token] {
			return false
		}
		if token[0] >= '0' && token[0] <= '9' {
			return false // a literal number
		}
		names = append(names, token)
		return false
	}

	inSelector := false
	for i := 0; i < len(promQL); i++ {
		ch := promQL[i]
		switch {
		case ch == '{':
			flush(false)
			inSelector = true
		case ch == '}':
			inSelector = false
		case inSelector:
			// Label matchers hold label names and quoted values, not metrics.
		case isIdentChar(ch):
			current.WriteByte(ch)
		default:
			// A function name may be separated from its parenthesis by
			// spaces — "sum by (x) (metric)" writes the aggregation that way —
			// so peek past whitespace rather than only checking this byte.
			if flush(peekIsParen(promQL, i)) {
				i = skipParenGroup(promQL, i)
			}
		}
	}
	flush(false)
	return names
}

// skipParenGroup advances past a "( ... )" starting at or after i, so the
// label names inside a by(...) clause are never read as metrics. It
// returns the index of the closing parenthesis, or the end of the input if
// the expression is unbalanced.
func skipParenGroup(promQL string, i int) int {
	for i < len(promQL) && promQL[i] != '(' {
		if !isSpace(promQL[i]) {
			return i // not actually a label list — leave the scanner where it was
		}
		i++
	}
	depth := 0
	for ; i < len(promQL); i++ {
		switch promQL[i] {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return len(promQL)
}

// peekIsParen reports whether the next non-space byte from i is "(".
func peekIsParen(promQL string, i int) bool {
	for ; i < len(promQL); i++ {
		if !isSpace(promQL[i]) {
			return promQL[i] == '('
		}
	}
	return false
}

func isSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func isIdentChar(ch byte) bool {
	return ch == '_' || ch == ':' ||
		(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}
