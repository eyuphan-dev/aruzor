package uptime

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"aruzor/internal/store"
)

// recordingNotifier stands in for the Telegram/push/webhook broadcaster.
type recordingNotifier struct{ sent []string }

func (n *recordingNotifier) SendAlert(_ context.Context, text string) error {
	n.sent = append(n.sent, text)
	return nil
}

func newTestChecker(t *testing.T) (*Checker, *store.Store, *recordingNotifier) {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("test veritabani acilamadi: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	notifier := &recordingNotifier{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewChecker(db, notifier, log), db, notifier
}

func seedCertMonitor(t *testing.T, db *store.Store, snoozedUntil *time.Time) store.Monitor {
	t.Helper()
	ctx := context.Background()
	m := store.Monitor{
		ID: uuid.NewString(), Name: "ornek.com", Type: "http",
		Target: "https://ornek.com", IntervalSeconds: 60,
	}
	if err := db.CreateMonitor(ctx, m); err != nil {
		t.Fatalf("izleme olusturulamadi: %v", err)
	}
	if snoozedUntil != nil {
		if err := db.SetMonitorSnooze(ctx, m.ID, snoozedUntil); err != nil {
			t.Fatalf("susturma ayarlanamadi: %v", err)
		}
		m.SnoozedUntil = snoozedUntil
	}
	return m
}

// The whole path: an expiring certificate reaches the notifier once, and the
// fact that it was announced is persisted so the next check — fifteen
// seconds later — stays quiet.
func TestWarnOnExpiringCert_BirKezGonderilirVeKaydedilir(t *testing.T) {
	ctx := context.Background()
	c, db, notifier := newTestChecker(t)
	m := seedCertMonitor(t, db, nil)

	expiry := time.Now().Add(3 * 24 * time.Hour)
	c.warnOnExpiringCert(ctx, m, &expiry)

	if len(notifier.sent) != 1 {
		t.Fatalf("gonderilen mesaj sayisi = %d, 1 bekleniyordu", len(notifier.sent))
	}
	if !strings.Contains(notifier.sent[0], "ornek.com") {
		t.Errorf("mesaj izleme adini icermiyor: %q", notifier.sent[0])
	}

	// Re-read the monitor the way the checker does on its next pass.
	reloaded := reloadMonitor(t, db, m.ID)
	if reloaded.CertWarnedFor == nil {
		t.Fatal("uyarilan sertifika tarihi kaydedilmemis")
	}
	c.warnOnExpiringCert(ctx, reloaded, &expiry)
	if len(notifier.sent) != 1 {
		t.Fatalf("ayni sertifika icin ikinci mesaj gonderildi: %v", notifier.sent)
	}
}

// A maintenance window should delay the warning, not swallow it: the state
// must stay unwritten so the message goes out once the snooze ends.
func TestWarnOnExpiringCert_SusturulmusIzlemeKaydedilmez(t *testing.T) {
	ctx := context.Background()
	c, db, notifier := newTestChecker(t)
	until := time.Now().Add(time.Hour)
	m := seedCertMonitor(t, db, &until)

	expiry := time.Now().Add(2 * 24 * time.Hour)
	c.warnOnExpiringCert(ctx, m, &expiry)

	if len(notifier.sent) != 0 {
		t.Fatalf("susturulmus izleme icin mesaj gonderildi: %v", notifier.sent)
	}
	if reloadMonitor(t, db, m.ID).CertWarnedFor != nil {
		t.Error("gonderilmeyen uyari kaydedilmis, susturma bitince mesaj kaybolur")
	}
}

func TestWarnOnExpiringCert_UzakTarihSessiz(t *testing.T) {
	ctx := context.Background()
	c, db, notifier := newTestChecker(t)
	m := seedCertMonitor(t, db, nil)

	expiry := time.Now().Add(58 * 24 * time.Hour)
	c.warnOnExpiringCert(ctx, m, &expiry)

	if len(notifier.sent) != 0 {
		t.Fatalf("58 gun kalan sertifika icin mesaj gonderildi: %v", notifier.sent)
	}
}

func reloadMonitor(t *testing.T, db *store.Store, id string) store.Monitor {
	t.Helper()
	monitors, err := db.ListMonitors(context.Background())
	if err != nil {
		t.Fatalf("izlemeler okunamadi: %v", err)
	}
	for _, m := range monitors {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("izleme bulunamadi: %s", id)
	return store.Monitor{}
}
