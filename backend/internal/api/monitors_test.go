package api

import "testing"

// validateCustomCheckFields is the whole gate between a monitor's config
// and the database — every reject case here is a case that would otherwise
// save a monitor that looks configured but silently does the wrong thing
// (or nothing) forever.
func TestValidateCustomCheckFields(t *testing.T) {
	rejected := []struct {
		name string
		req  createMonitorRequest
	}{
		{
			"tcp izlemede metod dolu",
			createMonitorRequest{Type: "tcp", Method: "POST"},
		},
		{
			"tcp izlemede govde dolu",
			createMonitorRequest{Type: "tcp", RequestBody: "{}"},
		},
		{
			"tcp izlemede beklenen durum dolu",
			createMonitorRequest{Type: "tcp", ExpectedStatus: "200"},
		},
		{
			"tcp izlemede beklenen icerik dolu",
			createMonitorRequest{Type: "tcp", ExpectBodyContains: "ok"},
		},
		{
			"bilinmeyen metod",
			createMonitorRequest{Type: "http", Method: "TRACE"},
		},
		{
			"HEAD ile beklenen icerik birlikte",
			createMonitorRequest{Type: "http", Method: "HEAD", ExpectBodyContains: "hos geldiniz"},
		},
		{
			"beklenen durum sayi degil",
			createMonitorRequest{Type: "http", ExpectedStatus: "abc"},
		},
		{
			"beklenen durum araligin disinda",
			createMonitorRequest{Type: "http", ExpectedStatus: "999"},
		},
		{
			"beklenen durum listesinde bir tanesi bozuk",
			createMonitorRequest{Type: "http", ExpectedStatus: "200, x"},
		},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateCustomCheckFields(tc.req); err == nil {
				t.Errorf("reddedilmesi beklenirdi: %+v", tc.req)
			}
		})
	}

	accepted := []struct {
		name string
		req  createMonitorRequest
	}{
		{"tum alanlar bos, tcp", createMonitorRequest{Type: "tcp"}},
		{"tum alanlar bos, http", createMonitorRequest{Type: "http"}},
		{
			"gecerli POST + govde + beklenen durum + beklenen icerik",
			createMonitorRequest{
				Type: "http", Method: "POST", RequestBody: `{"a":1}`,
				ExpectedStatus: "200,302", ExpectBodyContains: "hos geldiniz",
			},
		},
		{"HEAD, icerik doğrulamasi yok", createMonitorRequest{Type: "http", Method: "HEAD"}},
		{"bosluklu beklenen durum listesi", createMonitorRequest{Type: "http", ExpectedStatus: " 200 , 302 "}},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateCustomCheckFields(tc.req); err != nil {
				t.Errorf("kabul edilmesi beklenirdi, hata: %v (%+v)", err, tc.req)
			}
		})
	}
}

func TestValidateCustomCheckFieldsRequestBodyTooLarge(t *testing.T) {
	big := make([]byte, maxRequestBodyLen+1)
	req := createMonitorRequest{Type: "http", RequestBody: string(big)}
	if err := validateCustomCheckFields(req); err != errRequestBodyTooLarge {
		t.Errorf("hata = %v, beklenen %v", err, errRequestBodyTooLarge)
	}
}
