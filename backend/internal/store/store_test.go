package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	// A fresh in-memory database per test — file-based would leak state
	// (and disk) across the whole suite for no benefit here.
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("test veritabani acilamadi: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedMonitor(t *testing.T, s *Store, ctx context.Context) string {
	t.Helper()
	id := uuid.NewString()
	if err := s.CreateMonitor(ctx, Monitor{ID: id, Name: "test", Type: "http", Target: "https://example.com", IntervalSeconds: 60}); err != nil {
		t.Fatalf("izleme olusturulamadi: %v", err)
	}
	return id
}

func recordCheck(t *testing.T, s *Store, ctx context.Context, monitorID string, ok bool, at time.Time) {
	t.Helper()
	c := MonitorCheck{ID: uuid.NewString(), MonitorID: monitorID, OK: ok, LatencyMs: 100, Attempts: 1, CheckedAt: at}
	if err := s.RecordMonitorCheck(ctx, c, nil); err != nil {
		t.Fatalf("kontrol kaydedilemedi: %v", err)
	}
}

// A window with no checks at all must not be reported as 0% — that would
// read as "always down" for a monitor that simply hasn't run yet.
func TestGetUptimeSummaryOmitsWindowsWithNoHistory(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	id := seedMonitor(t, s, ctx)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	summary, err := s.GetUptimeSummary(ctx, id, now)
	if err != nil {
		t.Fatalf("ozet alinamadi: %v", err)
	}
	if summary.Day24h != nil || summary.Day7 != nil || summary.Day30 != nil || summary.Day90 != nil {
		t.Fatalf("hic kontrol yokken tum pencereler nil olmali: %+v", summary)
	}

	// A single recent check falls inside every window at once (24h through
	// 90d are nested), so all four should now report the same figure.
	recordCheck(t, s, ctx, id, true, now.Add(-90*time.Minute))
	recordCheck(t, s, ctx, id, false, now.Add(-30*time.Minute))

	summary, err = s.GetUptimeSummary(ctx, id, now)
	if err != nil {
		t.Fatalf("ozet alinamadi: %v", err)
	}
	for name, got := range map[string]*float64{"24h": summary.Day24h, "7g": summary.Day7, "30g": summary.Day30, "90g": summary.Day90} {
		if got == nil {
			t.Fatalf("%s penceresi veri icermeli", name)
		}
		if *got != 50 {
			t.Fatalf("%s yuzdesi = %v, beklenen 50", name, *got)
		}
	}
}

// The strip must have one entry per day even for days with zero checks —
// dropping them would silently shrink the strip and misalign it with dates.
func TestDailyUptimeHistoryFillsGaps(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	id := seedMonitor(t, s, ctx)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	// Checks only on "today" and three days ago; the days between must
	// still appear, with Total 0.
	recordCheck(t, s, ctx, id, true, now)
	recordCheck(t, s, ctx, id, true, now)
	recordCheck(t, s, ctx, id, false, now.AddDate(0, 0, -3))

	history, err := s.DailyUptimeHistory(ctx, id, 7, now)
	if err != nil {
		t.Fatalf("gecmis alinamadi: %v", err)
	}
	if len(history) != 7 {
		t.Fatalf("7 gunluk gecmis 7 girdi icermeli, %d geldi", len(history))
	}

	last := history[len(history)-1]
	if last.Total != 2 || last.Percent != 100 {
		t.Fatalf("bugun: total=%d yuzde=%v, beklenen total=2 yuzde=100", last.Total, last.Percent)
	}

	threeAgo := history[len(history)-4]
	if threeAgo.Total != 1 || threeAgo.Failed != 1 || threeAgo.Percent != 0 {
		t.Fatalf("3 gun once: total=%d failed=%d yuzde=%v, beklenen total=1 failed=1 yuzde=0",
			threeAgo.Total, threeAgo.Failed, threeAgo.Percent)
	}

	gapDay := history[len(history)-2]
	if gapDay.Total != 0 {
		t.Fatalf("kontrol yapilmayan gun total=0 olmali, %d geldi", gapDay.Total)
	}
}

func TestPushSubscriptionUpsertReplacesKeys(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	endpoint := "https://push.example.com/abc"
	if err := s.UpsertPushSubscription(ctx, endpoint, "key1", "auth1"); err != nil {
		t.Fatalf("kayit olusturulamadi: %v", err)
	}
	// Re-subscribing the same endpoint (permission re-granted) must replace
	// the keys in place, not create a second row that would double-deliver.
	if err := s.UpsertPushSubscription(ctx, endpoint, "key2", "auth2"); err != nil {
		t.Fatalf("kayit guncellenemedi: %v", err)
	}

	subs, err := s.ListPushSubscriptions(ctx)
	if err != nil {
		t.Fatalf("liste alinamadi: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("bir kayit beklenirdi, %d geldi", len(subs))
	}
	if subs[0].P256dh != "key2" || subs[0].Auth != "auth2" {
		t.Fatalf("anahtarlar guncellenmemis: %+v", subs[0])
	}

	if err := s.DeletePushSubscription(ctx, endpoint); err != nil {
		t.Fatalf("kayit silinemedi: %v", err)
	}
	subs, err = s.ListPushSubscriptions(ctx)
	if err != nil {
		t.Fatalf("liste alinamadi: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("silme sonrasi kayit kalmamali, %d geldi", len(subs))
	}
}
