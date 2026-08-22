package uptime

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"testing"
	"time"
)

func at(base time.Time, minutes int) time.Time {
	return base.Add(time.Duration(minutes) * time.Minute)
}

// The notification policy is the part of this package that fails silently:
// too eager and the chat fills with noise until people mute it, too shy and
// an outage passes unreported. Both failures look like "nothing happened".
func TestDecideAlertState(t *testing.T) {
	base := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	fiveAgo := at(base, -5)
	oneAgo := at(base, -1)

	cases := []struct {
		name         string
		state        string
		failingSince *time.Time
		ok           bool
		class        string
		wantState    string
		wantMessage  bool
	}{
		{"saglikli kalirken sessiz", "ok", nil, true, ClassOK, "ok", false},
		{"ilk basarisizlik bildirilmez", "ok", nil, false, ClassTimeoutConnect, "ok", false},
		{"esik dolmadan bildirilmez", "ok", &oneAgo, false, ClassTimeoutConnect, "ok", false},
		{"esik dolunca bildirilir", "ok", &fiveAgo, false, ClassTimeoutConnect, "down", true},
		{"cokmus halde tekrarlanmaz", "down", &fiveAgo, false, ClassTimeoutConnect, "down", false},
		{"toparlayinca bir kez bildirilir", "down", &fiveAgo, true, ClassOK, "ok", true},
		{"toparladiktan sonra sessiz", "ok", nil, true, ClassOK, "ok", false},

		// The exact case that made the chat unusable: a stall that clears
		// before the threshold must leave no trace in Telegram at all.
		{"kisa takilma sessiz gecer", "ok", &oneAgo, true, ClassOK, "ok", false},

		// Being blocked says the site refused us, not that it is down.
		{"engellenme hic bildirilmez", "ok", &fiveAgo, false, ClassHTTPBlocked, "ok", false},

		// Rows written before the alert_state column existed read as empty.
		{"bos durum saglikli sayilir", "", nil, true, ClassOK, "ok", false},
		{"bos durum ilk hatada bildirmez", "", nil, false, ClassRefused, "ok", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := decideAlertState(alertInput{
				state: c.state, failingSince: c.failingSince, ok: c.ok,
				errorClass: c.class, name: "test", now: base,
			})
			if d.state != c.wantState {
				t.Errorf("durum = %q, beklenen %q", d.state, c.wantState)
			}
			if (d.message != "") != c.wantMessage {
				t.Errorf("mesaj = %q, bildirim beklendi mi: %v", d.message, c.wantMessage)
			}
		})
	}
}

// The failure clock has to start at the first failure and survive later
// ones untouched, because the whole policy is measured from it.
func TestFailingSinceStartsOnceAndClearsOnSuccess(t *testing.T) {
	base := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	first := decideAlertState(alertInput{state: "ok", ok: false, errorClass: ClassRefused, name: "t", now: base})
	if first.failingSince == nil || !first.failingSince.Equal(base) {
		t.Fatalf("ilk hatada saat baslamadi: %v", first.failingSince)
	}

	later := decideAlertState(alertInput{
		state: "ok", failingSince: first.failingSince, ok: false,
		errorClass: ClassRefused, name: "t", now: at(base, 3),
	})
	if !later.failingSince.Equal(base) {
		t.Fatalf("saat sifirlandi: %v, beklenen %v", later.failingSince, base)
	}

	recovered := decideAlertState(alertInput{
		state: "ok", failingSince: first.failingSince, ok: true, name: "t", now: at(base, 4),
	})
	if recovered.failingSince != nil {
		t.Fatalf("basarili kontrolde saat temizlenmedi: %v", recovered.failingSince)
	}
}

// Replays the real day that prompted this policy: seven separate stalls of
// two to six minutes on a site that stayed usable throughout. The old
// count-based rule sent fourteen messages for this. Only the six-minute
// stall is a genuine outage by the new rule.
func TestRealWorldStallsDoNotFloodTheChat(t *testing.T) {
	base := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	stalls := []int{2, 2, 4, 3, 3, 3, 6} // minutes, as observed in production

	state := "ok"
	var failingSince *time.Time
	messages := 0
	minute := 0

	for _, length := range stalls {
		for i := 0; i < length; i++ {
			d := decideAlertState(alertInput{
				state: state, failingSince: failingSince, ok: false,
				errorClass: ClassTimeoutConnect, name: "t", now: at(base, minute),
			})
			state, failingSince = d.state, d.failingSince
			if d.message != "" {
				messages++
			}
			minute++
		}
		d := decideAlertState(alertInput{
			state: state, failingSince: failingSince, ok: true, name: "t", now: at(base, minute),
		})
		state, failingSince = d.state, d.failingSince
		if d.message != "" {
			messages++
		}
		minute += 30 // healthy stretch between stalls
	}

	// One outage announcement plus its recovery, for the six-minute stall.
	if messages != 2 {
		t.Fatalf("mesaj sayisi = %d, beklenen 2 (yalnizca 6 dakikalik kesinti)", messages)
	}
	if state != "ok" {
		t.Fatalf("son durum = %q, beklenen ok", state)
	}
}

// A genuine outage still has to be reported, and reported once.
func TestSustainedOutageIsReportedOnce(t *testing.T) {
	base := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	state := "ok"
	var failingSince *time.Time
	messages := 0

	for minute := 0; minute < 90; minute++ {
		d := decideAlertState(alertInput{
			state: state, failingSince: failingSince, ok: false,
			errorClass: ClassRefused, name: "t", now: at(base, minute),
		})
		state, failingSince = d.state, d.failingSince
		if d.message != "" {
			messages++
		}
	}
	if messages != 1 {
		t.Fatalf("90 dakikalik kesinti icin mesaj sayisi = %d, beklenen 1", messages)
	}
	if state != "down" {
		t.Fatalf("durum = %q, beklenen down", state)
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Minute, "5 dakika"},
		{59 * time.Minute, "59 dakika"},
		{60 * time.Minute, "1 saat"},
		{95 * time.Minute, "1 saat 35 dakika"},
		{120 * time.Minute, "2 saat"},
	}
	for _, c := range cases {
		if got := humanDuration(c.d); got != c.want {
			t.Errorf("humanDuration(%v) = %q, beklenen %q", c.d, got, c.want)
		}
	}
}

// classifyStatus decides what counts as up. The ranges are the contract the
// rest of the system is built on, so they are pinned here.
func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		code      int
		wantOK    bool
		wantClass string
	}{
		{200, true, ClassOK},
		{204, true, ClassOK},
		{301, true, ClassOK},
		{302, true, ClassOK}, // a redirect to a login page is a healthy site
		{403, false, ClassHTTPBlocked},
		{429, false, ClassHTTPBlocked},
		{404, false, ClassHTTPStatus},
		{500, false, ClassHTTPServerError},
		{502, false, ClassHTTPServerError},
		{503, false, ClassHTTPServerError},
	}
	for _, c := range cases {
		ok, class, detail := classifyStatus(c.code)
		if ok != c.wantOK || class != c.wantClass {
			t.Errorf("HTTP %d -> ok=%v sinif=%q, beklenen ok=%v sinif=%q", c.code, ok, class, c.wantOK, c.wantClass)
		}
		if !ok && detail == "" {
			t.Errorf("HTTP %d basarisiz ama aciklama bos", c.code)
		}
	}
}

// A timeout means two completely different things depending on whether the
// connection was ever established, and the whole diagnosis rests on telling
// them apart.
func TestClassifyTimeoutSplitsOnConnection(t *testing.T) {
	class, _ := classifyError(context.DeadlineExceeded, false)
	if class != ClassTimeoutConnect {
		t.Errorf("baglanti kurulmadan zaman asimi = %q, beklenen %q", class, ClassTimeoutConnect)
	}
	class, _ = classifyError(context.DeadlineExceeded, true)
	if class != ClassTimeoutResponse {
		t.Errorf("baglantidan sonra zaman asimi = %q, beklenen %q", class, ClassTimeoutResponse)
	}
}

func TestClassifyError(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		connected bool
		want      string
	}{
		{"dns", &net.DNSError{Err: "no such host", Name: "yok.example"}, false, ClassDNS},
		{"reddedildi", errors.New("dial tcp 1.2.3.4:443: connect: connection refused"), false, ClassRefused},
		{"suresi dolmus sertifika", x509.CertificateInvalidError{Reason: x509.Expired}, false, ClassTLSCert},
		{"yanlis alan adi", x509.HostnameError{Host: "yanlis.example"}, false, ClassTLSCert},
		{"taninmayan otorite", x509.UnknownAuthorityError{}, false, ClassTLSCert},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			class, detail := classifyError(c.err, c.connected)
			if class != c.want {
				t.Errorf("sinif = %q, beklenen %q", class, c.want)
			}
			if detail == "" {
				t.Error("aciklama bos")
			}
		})
	}
}

// Every failure class has to produce a sentence. A class that falls through
// to an empty label would send a Telegram message that says nothing.
func TestCauseLabelCoversEveryClass(t *testing.T) {
	classes := []string{
		ClassDNS, ClassRefused, ClassTimeoutConnect, ClassTimeoutResponse,
		ClassTLSCert, ClassTLSHandshake, ClassHTTPServerError, ClassHTTPBlocked,
		ClassHTTPStatus, ClassUnknown,
	}
	for _, c := range classes {
		if label := causeLabel(c, "ayrinti"); label == "" {
			t.Errorf("%q sinifi icin etiket bos", c)
		}
	}
	if causeLabel(ClassUnknown, "") == "" {
		t.Error("ayrinti yokken bile bir sey soylenmeli")
	}
}
