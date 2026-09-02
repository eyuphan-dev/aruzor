package api

import (
	"testing"
	"time"

	"aruzor/internal/store"
)

func bucket(at time.Time, requests, bytes, s5xx int64) store.TrafficBucket {
	return store.TrafficBucket{At: at, Requests: requests, Bytes: bytes, S5xx: s5xx, S2xx: requests - s5xx}
}

func TestRollUp_AdimaToplar(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	buckets := []store.TrafficBucket{
		bucket(base, 10, 100, 1),
		bucket(base.Add(time.Minute), 20, 200, 0),
		bucket(base.Add(2*time.Minute), 30, 300, 2),
		bucket(base.Add(5*time.Minute), 40, 400, 0),
	}

	points := rollUp(buckets, 5*time.Minute)
	if len(points) != 2 {
		t.Fatalf("nokta sayisi = %d, 2 bekleniyordu", len(points))
	}
	if points[0].Requests != 60 || points[0].Bytes != 600 || points[0].S5xx != 3 {
		t.Errorf("ilk adim yanlis toplanmis: %+v", points[0])
	}
	if points[1].Requests != 40 {
		t.Errorf("ikinci adim yanlis: %+v", points[1])
	}
}

// A minute in which nothing was served is a real fact — the server was up
// and quiet. Skipping it would leave a gap in the line, which reads as
// "the collector stopped".
func TestRollUp_SessizAralikSifirlaDoldurulur(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	points := rollUp([]store.TrafficBucket{
		bucket(base, 10, 100, 0),
		bucket(base.Add(30*time.Minute), 10, 100, 0),
	}, 10*time.Minute)

	if len(points) != 4 {
		t.Fatalf("nokta sayisi = %d, aradaki bos adimlar da uretilmeliydi", len(points))
	}
	if points[1].Requests != 0 || points[2].Requests != 0 {
		t.Errorf("bos adimlar sifir olmali: %+v %+v", points[1], points[2])
	}
	for i := 1; i < len(points); i++ {
		if points[i].At <= points[i-1].At {
			t.Fatalf("noktalar zaman sirasinda degil: %d <= %d", points[i].At, points[i-1].At)
		}
	}
}

func TestRollUp_VeriYokkenBosDizi(t *testing.T) {
	// nil would serialise as JSON null and make the chart component branch
	// on it; an empty array is the same fact in a shape the UI already
	// handles.
	if got := rollUp(nil, time.Minute); got == nil || len(got) != 0 {
		t.Errorf("bos girdide bos dizi donmeli, alinan %v", got)
	}
}

func TestTotalsFrom(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	buckets := []store.TrafficBucket{
		{At: base, Requests: 100, Bytes: 1000, S5xx: 5, S4xx: 10, Unauthorized: 3, DurationMsSum: 5000, DurationCount: 100},
		{At: base.Add(time.Minute), Requests: 300, Bytes: 3000, S5xx: 15, S4xx: 0, Unauthorized: 0, DurationMsSum: 15000, DurationCount: 300},
	}

	tot := totalsFrom(buckets, time.Hour, time.Minute)
	if tot.Requests != 400 || tot.Bytes != 4000 || tot.Errors5xx != 20 || tot.Errors4xx != 10 || tot.Unauthorized != 3 {
		t.Errorf("toplamlar yanlis: %+v", tot)
	}
	// Rates average over the whole requested window, not over the minutes
	// that happen to have rows: "requests per second over the last hour"
	// must mean that even when the server was idle for most of it.
	if want := 400.0 / 3600.0; tot.RequestsPerS != want {
		t.Errorf("istek/sn = %v, beklenen %v", tot.RequestsPerS, want)
	}
	if want := 4000.0 / 3600.0; tot.BytesPerS != want {
		t.Errorf("bayt/sn = %v, beklenen %v", tot.BytesPerS, want)
	}
	// The peak is per step, and answers a different question: how busy did
	// it get at its busiest.
	if want := 300.0 / 60.0; tot.PeakRequests != want {
		t.Errorf("tepe istek/sn = %v, beklenen %v", tot.PeakRequests, want)
	}
	if want := 5.0; tot.ErrorRate != want {
		t.Errorf("hata orani = %v, beklenen %v", tot.ErrorRate, want)
	}
	if !tot.HasDuration || tot.AvgDurationMs != 50 {
		t.Errorf("ortalama sure = %v (var mi: %v), beklenen 50", tot.AvgDurationMs, tot.HasDuration)
	}
}

// The stock combined log format carries no $request_time. Reporting an
// average of zero would be a measurement nobody made; the flag is what lets
// the UI say "add this to your log_format" instead.
func TestTotalsFrom_SureYokkenIsaretlenmez(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	tot := totalsFrom([]store.TrafficBucket{{At: base, Requests: 10}}, time.Hour, time.Minute)
	if tot.HasDuration || tot.AvgDurationMs != 0 {
		t.Errorf("sure verisi yokken bildirilmis: %+v", tot)
	}
}

func TestTotalsFrom_IstekYokkenHataOraniSifir(t *testing.T) {
	tot := totalsFrom(nil, time.Hour, time.Minute)
	if tot.ErrorRate != 0 || tot.Requests != 0 {
		t.Errorf("bos pencerede sifira bolme: %+v", tot)
	}
}

func TestTrafficRanges_HepsiPozitif(t *testing.T) {
	if _, ok := trafficRanges[defaultTrafficRange]; !ok {
		t.Fatalf("varsayilan aralik %q tabloda yok", defaultTrafficRange)
	}
	for key, spec := range trafficRanges {
		if spec.window <= 0 || spec.step <= 0 {
			t.Errorf("%s: pencere=%v adim=%v", key, spec.window, spec.step)
		}
		// A step longer than the window would produce a single point, and a
		// step that does not divide it would produce a ragged last bucket.
		if spec.step > spec.window {
			t.Errorf("%s: adim pencereden buyuk", key)
		}
	}
}
