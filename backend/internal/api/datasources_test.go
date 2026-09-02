package api

import "testing"

func TestValidateDatasourceURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"gecerli http", "http://localhost:9090", true},
		{"gecerli https", "https://prometheus.ornek.com", true},
		{"sema yok", "localhost:9090", false},
		{"bos", "", false},
		{"bilinmeyen sema", "ftp://ornek.com", false},
		{"host yok", "http://", false},
		{"sadece yol", "/api/v1/query", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateDatasourceURL(c.url)
			if (err == nil) != c.want {
				t.Errorf("validateDatasourceURL(%q) hata=%v, beklenen gecerli=%v", c.url, err, c.want)
			}
		})
	}
}
