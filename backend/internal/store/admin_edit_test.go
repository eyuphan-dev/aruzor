package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestUpdateMonitor_HedefVeAlanlariDegistirir(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	id := seedMonitor(t, s, ctx)

	err := s.UpdateMonitor(ctx, Monitor{
		ID: id, Name: "yeni ad", Type: "http", Target: "https://yeni.example.com",
		IntervalSeconds: 30, Method: "POST", ExpectedStatus: "200,302",
	})
	if err != nil {
		t.Fatalf("izleme duzenlenemedi: %v", err)
	}

	monitors, err := s.ListMonitors(ctx)
	if err != nil {
		t.Fatalf("izlemeler okunamadi: %v", err)
	}
	if len(monitors) != 1 {
		t.Fatalf("izleme sayisi = %d", len(monitors))
	}
	m := monitors[0]
	if m.Name != "yeni ad" || m.Target != "https://yeni.example.com" || m.IntervalSeconds != 30 ||
		m.Method != "POST" || m.ExpectedStatus != "200,302" {
		t.Errorf("duzenleme uygulanmamis: %+v", m)
	}
}

func TestUpdateMonitor_UyariDurumunuKorur(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	id := seedMonitor(t, s, ctx)
	since := time.Now().Add(-time.Hour)
	if err := s.SetMonitorAlertState(ctx, id, "down", 3, &since); err != nil {
		t.Fatalf("uyari durumu ayarlanamadi: %v", err)
	}

	if err := s.UpdateMonitor(ctx, Monitor{ID: id, Name: "x", Type: "http", Target: "https://x.example.com", IntervalSeconds: 60}); err != nil {
		t.Fatalf("izleme duzenlenemedi: %v", err)
	}

	monitors, _ := s.ListMonitors(ctx)
	if monitors[0].AlertState != "down" || monitors[0].ConsecutiveFailures != 3 {
		t.Errorf("tanim duzenlemesi izleme gecmisini bozdu: %+v", monitors[0])
	}
}

func TestUpdateAlertRule_KismiAlanlariDegistirir(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	id := uuid.NewString()
	if err := s.CreateAlertRule(ctx, AlertRule{ID: id, Name: "cpu", PromQL: "up", Operator: ">", Threshold: 90, Enabled: true}); err != nil {
		t.Fatalf("kural olusturulamadi: %v", err)
	}

	if err := s.UpdateAlertRule(ctx, id, "cpu yuksek", "up", ">=", 95); err != nil {
		t.Fatalf("kural duzenlenemedi: %v", err)
	}

	rules, err := s.ListAlertRules(ctx)
	if err != nil {
		t.Fatalf("kurallar okunamadi: %v", err)
	}
	if rules[0].Name != "cpu yuksek" || rules[0].Operator != ">=" || rules[0].Threshold != 95 {
		t.Errorf("duzenleme uygulanmamis: %+v", rules[0])
	}
}

func TestSetAlertRulePending_YazilirVeOkunur(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	id := uuid.NewString()
	if err := s.CreateAlertRule(ctx, AlertRule{ID: id, Name: "x", PromQL: "up", Operator: ">", Threshold: 1, Enabled: true}); err != nil {
		t.Fatalf("kural olusturulamadi: %v", err)
	}

	since := time.Now().Add(-time.Minute)
	if err := s.SetAlertRulePending(ctx, id, "firing", &since); err != nil {
		t.Fatalf("bekleme durumu yazilamadi: %v", err)
	}
	rules, _ := s.ListAlertRules(ctx)
	if rules[0].PendingState != "firing" || rules[0].PendingSince == nil {
		t.Fatalf("bekleme durumu okunamadi: %+v", rules[0])
	}

	// Committing a transition clears the pending breach — the countdown it
	// was tracking either just landed or no longer means anything.
	if err := s.UpdateAlertRuleState(ctx, id, "firing", time.Now()); err != nil {
		t.Fatalf("durum guncellenemedi: %v", err)
	}
	rules, _ = s.ListAlertRules(ctx)
	if rules[0].PendingState != "" || rules[0].PendingSince != nil {
		t.Errorf("taahhutten sonra bekleme durumu temizlenmedi: %+v", rules[0])
	}
}

func TestUpdateDatasource(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	id := uuid.NewString()
	if err := s.CreateDatasource(ctx, Datasource{ID: id, Name: "eski", URL: "http://eski:9090", Type: "prometheus"}); err != nil {
		t.Fatalf("veri kaynagi olusturulamadi: %v", err)
	}

	if err := s.UpdateDatasource(ctx, id, "yeni", "http://yeni:9090"); err != nil {
		t.Fatalf("veri kaynagi duzenlenemedi: %v", err)
	}

	ds, err := s.GetDatasource(ctx, id)
	if err != nil || ds == nil {
		t.Fatalf("veri kaynagi okunamadi: %v", err)
	}
	if ds.Name != "yeni" || ds.URL != "http://yeni:9090" {
		t.Errorf("duzenleme uygulanmamis: %+v", ds)
	}
}

func TestCountSuperAdmins(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if n, err := s.CountSuperAdmins(ctx); err != nil || n != 0 {
		t.Fatalf("bos tabloda sayim = %d, hata=%v", n, err)
	}

	id1 := uuid.NewString()
	if err := s.CreateUser(ctx, id1, "a@x.com", "hash", "super_admin"); err != nil {
		t.Fatalf("kullanici olusturulamadi: %v", err)
	}
	id2 := uuid.NewString()
	if err := s.CreateUser(ctx, id2, "b@x.com", "hash", "viewer"); err != nil {
		t.Fatalf("kullanici olusturulamadi: %v", err)
	}
	if n, err := s.CountSuperAdmins(ctx); err != nil || n != 1 {
		t.Fatalf("sayim = %d, hata=%v", n, err)
	}

	if err := s.UpdateUserRole(ctx, id2, "super_admin"); err != nil {
		t.Fatalf("rol guncellenemedi: %v", err)
	}
	if n, err := s.CountSuperAdmins(ctx); err != nil || n != 2 {
		t.Fatalf("rol guncellemesi sonrasi sayim = %d, hata=%v", n, err)
	}
}

func TestUpdateUserPassword_HashiDegistirir(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	id := uuid.NewString()
	if err := s.CreateUser(ctx, id, "x@y.com", "eski-hash", "viewer"); err != nil {
		t.Fatalf("kullanici olusturulamadi: %v", err)
	}

	if err := s.UpdateUserPassword(ctx, id, "yeni-hash"); err != nil {
		t.Fatalf("sifre guncellenemedi: %v", err)
	}

	u, err := s.GetUserByEmail(ctx, "x@y.com")
	if err != nil || u == nil {
		t.Fatalf("kullanici okunamadi: %v", err)
	}
	if u.PasswordHash != "yeni-hash" {
		t.Errorf("sifre hash'i degismemis: %q", u.PasswordHash)
	}
}

func TestPruneAuditLogs(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.InsertAuditLog(ctx, uuid.NewString(), nil, "eski@x.com", "login_failed", "", "1.1.1.1"); err != nil {
		t.Fatalf("log yazilamadi: %v", err)
	}
	if err := s.InsertAuditLog(ctx, uuid.NewString(), nil, "yeni@x.com", "login_failed", "", "1.1.1.1"); err != nil {
		t.Fatalf("log yazilamadi: %v", err)
	}

	// Directly backdate the first row — InsertAuditLog always stamps "now",
	// there is no other way to get an old row for this test.
	if _, err := s.db.ExecContext(ctx, `UPDATE audit_logs SET created_at = ? WHERE email = ?`,
		time.Now().Add(-100*24*time.Hour), "eski@x.com"); err != nil {
		t.Fatalf("test satiri gunceli tarih ile isaretlenemedi: %v", err)
	}

	if err := s.PruneAuditLogs(ctx, time.Now().Add(-90*24*time.Hour)); err != nil {
		t.Fatalf("budama basarisiz: %v", err)
	}

	logs, err := s.ListAuditLogs(ctx, 100)
	if err != nil {
		t.Fatalf("loglar okunamadi: %v", err)
	}
	if len(logs) != 1 || logs[0].Email != "yeni@x.com" {
		t.Errorf("budama sonrasi = %+v, yalnizca yeni kayit kalmaliydi", logs)
	}
}

func TestInsertAuditLog_DetayAlaniKaydedilir(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	uid := uuid.NewString()
	if err := s.InsertAuditLog(ctx, uuid.NewString(), &uid, "a@x.com", "datasource_created", "prod -> http://10.0.0.5:9090", "2.2.2.2"); err != nil {
		t.Fatalf("log yazilamadi: %v", err)
	}
	logs, err := s.ListAuditLogs(ctx, 10)
	if err != nil {
		t.Fatalf("loglar okunamadi: %v", err)
	}
	if len(logs) != 1 || logs[0].Detail != "prod -> http://10.0.0.5:9090" {
		t.Errorf("detay alani kaydedilmemis: %+v", logs)
	}
}
