package api

import "testing"

// dashboardHasQuery is the only thing standing between an unauthenticated
// endpoint and arbitrary PromQL against the datasource, so its failure modes
// matter more than its success ones.
func TestDashboardHasQuery(t *testing.T) {
	const definition = `{"panels":[
		{"promql":"up"},
		{"promql":"100 - (avg(rate(node_cpu_seconds_total{mode=\"idle\"}[5m])) * 100)"}
	]}`

	allowed := []string{
		"up",
		`100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`,
	}
	for _, q := range allowed {
		if !dashboardHasQuery(definition, q) {
			t.Errorf("panodaki sorgu reddedildi: %q", q)
		}
	}

	rejected := []struct {
		name  string
		query string
	}{
		{"bos sorgu", ""},
		{"panoda olmayan metrik", "node_memory_MemTotal_bytes"},
		{"etiketleri okuyan sorgu", `{__name__=~".+"}`},
		{"panodaki sorgunun on eki", "u"},
		{"panodaki sorgu genisletilmis", "up or node_memory_MemTotal_bytes"},
		{"bosluk eklenmis hali", " up"},
		{"buyuk harfli hali", "UP"},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			if dashboardHasQuery(definition, tc.query) {
				t.Errorf("panoda olmayan sorgu kabul edildi: %q", tc.query)
			}
		})
	}
}

func TestDashboardHasQueryRejectsBrokenDefinition(t *testing.T) {
	for _, definition := range []string{"", "not json", "[]", `{"panels":null}`} {
		if dashboardHasQuery(definition, "up") {
			t.Errorf("bozuk tanim sorguyu kabul etti: %q", definition)
		}
	}
}
