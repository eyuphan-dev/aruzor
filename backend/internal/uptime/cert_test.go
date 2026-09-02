package uptime

import (
	"strings"
	"testing"
	"time"
)

func certAt(now time.Time, d time.Duration) *time.Time {
	t := now.Add(d)
	return &t
}

func TestDecideCertWarning_PencereIcinde(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	d := decideCertWarning(certInput{name: "ornek.com", expiry: certAt(now, 3*24*time.Hour), now: now})

	if !d.notify {
		t.Fatal("3 gun kalan sertifika icin uyari gonderilmeliydi")
	}
	if !strings.Contains(d.message, "ornek.com") || !strings.Contains(d.message, "3 gün") {
		t.Errorf("mesaj = %q", d.message)
	}
}

func TestDecideCertWarning_PencereDisinda(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	// A certificate with weeks left is not worth interrupting anyone for;
	// the app shows it either way.
	if d := decideCertWarning(certInput{name: "x", expiry: certAt(now, 30*24*time.Hour), now: now}); d.notify {
		t.Error("30 gun kalan sertifika icin uyari gonderilmemeliydi")
	}
	// Exactly on the boundary counts as inside it.
	if d := decideCertWarning(certInput{name: "x", expiry: certAt(now, certWarningWindow), now: now}); !d.notify {
		t.Error("tam sinirdaki sertifika uyari uretmeliydi")
	}
}

// An expired certificate already fails the probe and is reported as an
// outage naming the certificate as the cause. A second message would say the
// same thing twice in different words.
func TestDecideCertWarning_SuresiDolmusOlanKesintidir(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	if d := decideCertWarning(certInput{name: "x", expiry: certAt(now, -time.Hour), now: now}); d.notify {
		t.Error("suresi dolmus sertifika icin ayrica uyari gonderilmemeliydi")
	}
}

func TestDecideCertWarning_SertifikaGozlenmediyse(t *testing.T) {
	now := time.Now()
	// TCP monitors and plain-HTTP targets never observe one.
	if d := decideCertWarning(certInput{name: "x", expiry: nil, now: now}); d.notify {
		t.Error("sertifika gozlenmemisken uyari uretildi")
	}
}

func TestDecideCertWarning_AyniSertifikaBirKezBildirilir(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	expiry := certAt(now, 2*24*time.Hour)

	first := decideCertWarning(certInput{name: "x", expiry: expiry, now: now})
	if !first.notify {
		t.Fatal("ilk uyari gonderilmeliydi")
	}
	// The monitor runs every few seconds; without this the chat would get
	// the same message hundreds of times a day.
	again := decideCertWarning(certInput{name: "x", expiry: expiry, warnedFor: expiry, now: now.Add(time.Hour)})
	if again.notify {
		t.Error("ayni sertifika icin ikinci kez uyari gonderildi")
	}
}

// Renewing resets the warning on its own: the new certificate has a
// different expiry, so it no longer matches the one already announced.
func TestDecideCertWarning_YenilemeUyariyiSifirlar(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	old := certAt(now, 2*24*time.Hour)
	renewed := certAt(now, 90*24*time.Hour)

	if d := decideCertWarning(certInput{name: "x", expiry: renewed, warnedFor: old, now: now}); d.notify {
		t.Error("yenilenen sertifika hemen uyari uretmemeli, henuz 90 gun var")
	}
	// ...and once the renewed one gets close in its turn, it does warn.
	later := now.Add(85 * 24 * time.Hour)
	if d := decideCertWarning(certInput{name: "x", expiry: renewed, warnedFor: old, now: later}); !d.notify {
		t.Error("yenilenen sertifika suresi yaklasinca uyari uretmeliydi")
	}
}

func TestHumanCertRemaining(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{5 * 24 * time.Hour, "5 gün"},
		{25 * time.Hour, "1 gün"},
		// Below a day it reads in hours: "0 gün" on the final day would be
		// both alarming and wrong.
		{5 * time.Hour, "5 saat"},
		{30 * time.Minute, "1 saatten az"},
	}
	for _, c := range cases {
		if got := humanCertRemaining(c.in); got != c.want {
			t.Errorf("humanCertRemaining(%v) = %q, beklenen %q", c.in, got, c.want)
		}
	}
}
