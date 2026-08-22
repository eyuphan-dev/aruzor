package discovery

import (
	"reflect"
	"testing"
)

func TestMetricNamesIn(t *testing.T) {
	cases := []struct {
		name   string
		promQL string
		want   []string
	}{
		{
			name:   "bare metric",
			promQL: "up",
			want:   []string{"up"},
		},
		{
			name:   "function call is not a metric",
			promQL: `sum(pg_stat_activity_count)`,
			want:   []string{"pg_stat_activity_count"},
		},
		{
			name:   "label matcher contents are ignored",
			promQL: `node_cpu_seconds_total{mode="idle",instance="host:9100"}`,
			want:   []string{"node_cpu_seconds_total"},
		},
		{
			name:   "numbers and durations are not metrics",
			promQL: `100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`,
			want:   []string{"node_cpu_seconds_total"},
		},
		{
			name:   "aggregation keywords are skipped",
			promQL: `sum(container_memory_usage_bytes) by (name)`,
			want:   []string{"container_memory_usage_bytes"},
		},
		{
			name:   "grouping label list before the body",
			promQL: `sum by (instance) (node_load1)`,
			want:   []string{"node_load1"},
		},
		{
			name:   "matching clause between two aggregations",
			promQL: `sum(pg_stat_database_blks_hit) / on(datname) sum(pg_stat_database_blks_read)`,
			want:   []string{"pg_stat_database_blks_hit", "pg_stat_database_blks_read"},
		},
		{
			name:   "several metrics in one expression",
			promQL: `(1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)) * 100`,
			want:   []string{"node_memory_MemAvailable_bytes", "node_memory_MemTotal_bytes"},
		},
		{
			name:   "time() is a function, not a metric",
			promQL: `(probe_ssl_earliest_cert_expiry - time()) / 86400`,
			want:   []string{"probe_ssl_earliest_cert_expiry"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := metricNamesIn(tc.promQL)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("metricNamesIn(%q) = %v, istenen %v", tc.promQL, got, tc.want)
			}
		})
	}
}

// Every catalog query must survive its own detection check: if a query
// references a metric the catalog does not use as a detection prefix, and
// that metric is absent, the panel is dropped. This test guards against a
// typo in a query silently removing a panel from every install.
func TestCatalogPanelsSurviveTheirOwnMetrics(t *testing.T) {
	for _, entry := range catalog {
		present := map[string]struct{}{}
		for _, p := range entry.integration.Panels {
			for _, n := range metricNamesIn(p.PromQL) {
				present[n] = struct{}{}
			}
		}
		kept := availablePanels(entry.integration.Panels, present)
		if len(kept) != len(entry.integration.Panels) {
			t.Errorf("%s: %d panelden %d tanesi kendi metrikleriyle bile elendi",
				entry.integration.ID, len(entry.integration.Panels), len(kept))
		}
	}
}

func TestCatalogHasDetectionPrefixes(t *testing.T) {
	for _, entry := range catalog {
		if len(entry.prefixes) == 0 {
			t.Errorf("%s: tespit ön eki tanımlı değil, hiçbir zaman bulunamaz", entry.integration.ID)
		}
		if len(entry.integration.Panels) == 0 {
			t.Errorf("%s: panel önerisi yok", entry.integration.ID)
		}
	}
}
