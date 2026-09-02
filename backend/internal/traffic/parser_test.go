package traffic

import (
	"strings"
	"testing"
	"time"
)

func TestParseLine_CombinedFormat(t *testing.T) {
	line := `185.216.145.162 - - [02/Sep/2026:08:44:11 +0000] "GET /api/mcp?x=1 HTTP/1.1" 404 146 "-" "Mozilla/5.0 (compatible; Infrawatch/1.0; +https://infrawat.ch/)"`

	e, err := ParseLine(line)
	if err != nil {
		t.Fatalf("cozumlenemedi: %v", err)
	}
	if e.IP != "185.216.145.162" {
		t.Errorf("ip = %q", e.IP)
	}
	if e.Method != "GET" {
		t.Errorf("metod = %q", e.Method)
	}
	if e.Path != "/api/mcp" {
		t.Errorf("yol = %q, sorgu dizesi ayiklanmali", e.Path)
	}
	if e.Status != 404 {
		t.Errorf("durum = %d", e.Status)
	}
	if e.Bytes != 146 {
		t.Errorf("bayt = %d", e.Bytes)
	}
	if e.Referer != "" {
		t.Errorf("referer = %q, nginx'in '-' degeri bos olmali", e.Referer)
	}
	if !strings.Contains(e.UserAgent, "Infrawatch") {
		t.Errorf("istemci = %q", e.UserAgent)
	}
	// The zone is part of the line, so this must not depend on the host's.
	want := time.Date(2026, 9, 2, 8, 44, 11, 0, time.UTC)
	if !e.At.UTC().Equal(want) {
		t.Errorf("zaman = %v, beklenen %v", e.At.UTC(), want)
	}
	// Fields the combined format does not carry must stay empty rather than
	// being invented, so the UI can tell the operator what to add.
	if e.Host != "" || e.Service != "" || e.HasDuration {
		t.Errorf("combined formatta olmayan alanlar dolduruldu: host=%q servis=%q sure=%v", e.Host, e.Service, e.HasDuration)
	}
}

func TestParseLine_GenisletilmisAlanlar(t *testing.T) {
	line := `10.0.0.5 - - [02/Sep/2026:08:44:11 +0300] "POST /login HTTP/1.1" 302 512 "https://ornek.com/" "curl/8.4.0" "panel.ornek.com" 0.271 "127.0.0.1:3010"`

	e, err := ParseLine(line)
	if err != nil {
		t.Fatalf("cozumlenemedi: %v", err)
	}
	if e.Host != "panel.ornek.com" {
		t.Errorf("host = %q", e.Host)
	}
	if e.Service != "127.0.0.1:3010" {
		t.Errorf("servis = %q", e.Service)
	}
	if !e.HasDuration || e.DurationMs != 271 {
		t.Errorf("sure = %d ms (var mi: %v), beklenen 271", e.DurationMs, e.HasDuration)
	}
}

// The extra fields are matched by shape, not by position, so an operator
// who lists them in a different order still gets working panels.
func TestParseLine_GenisletilmisAlanlarSirasiz(t *testing.T) {
	line := `10.0.0.5 - - [02/Sep/2026:08:44:11 +0300] "GET / HTTP/1.1" 200 10 "-" "-" 1.5 "127.0.0.1:8080" "ornek.com"`

	e, err := ParseLine(line)
	if err != nil {
		t.Fatalf("cozumlenemedi: %v", err)
	}
	if e.Host != "ornek.com" || e.Service != "127.0.0.1:8080" || e.DurationMs != 1500 {
		t.Errorf("sirasiz alanlar yanlis eslendi: host=%q servis=%q sure=%d", e.Host, e.Service, e.DurationMs)
	}
}

func TestParseLine_Reddedilenler(t *testing.T) {
	cases := []struct{ name, line string }{
		{"bos", ""},
		{"cok kisa", `1.2.3.4 - - [02/Sep/2026:08:44:11 +0000]`},
		{"bozuk zaman", `1.2.3.4 - - [dun] "GET / HTTP/1.1" 200 10 "-" "-"`},
		{"durum kodu sayi degil", `1.2.3.4 - - [02/Sep/2026:08:44:11 +0000] "GET / HTTP/1.1" abc 10 "-" "-"`},
		{"durum kodu araligin disinda", `1.2.3.4 - - [02/Sep/2026:08:44:11 +0000] "GET / HTTP/1.1" 42 10 "-" "-"`},
		{"nginx hata logu satiri", `2026/09/02 08:44:11 [error] 123#0: *1 open() failed`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseLine(c.line); err == nil {
				t.Errorf("cozumlenmemeliydi ama basarili oldu")
			}
		})
	}
}

// A request line is attacker-controlled and frequently is not a request at
// all — TLS handshake bytes on a plain-HTTP port, for instance. Those must
// collapse into one bucket instead of each becoming its own path.
func TestParseLine_BozukIstekSatiri(t *testing.T) {
	for _, req := range []string{"", "garbage", `\x16\x03\x01`} {
		line := `1.2.3.4 - - [02/Sep/2026:08:44:11 +0000] "` + req + `" 400 0 "-" "-"`
		e, err := ParseLine(line)
		if err != nil {
			t.Fatalf("%q: cozumlenemedi: %v", req, err)
		}
		if e.Method != "-" || e.Path != "-" {
			t.Errorf("%q: metod=%q yol=%q, tek bir kovaya dusmeliydi", req, e.Method, e.Path)
		}
	}
}

// $body_bytes_sent is "-" when nothing was sent; that must be zero bytes,
// not a parse failure that loses the whole request.
func TestParseLine_BaytYok(t *testing.T) {
	line := `1.2.3.4 - - [02/Sep/2026:08:44:11 +0000] "GET / HTTP/1.1" 499 - "-" "-"`
	e, err := ParseLine(line)
	if err != nil {
		t.Fatalf("cozumlenemedi: %v", err)
	}
	if e.Bytes != 0 || e.Status != 499 {
		t.Errorf("bayt=%d durum=%d", e.Bytes, e.Status)
	}
}

func TestParseLine_KullaniciAjaninaGomuluTirnak(t *testing.T) {
	line := `1.2.3.4 - - [02/Sep/2026:08:44:11 +0000] "GET / HTTP/1.1" 200 5 "-" "Mozilla \"fake\" Bot"`
	e, err := ParseLine(line)
	if err != nil {
		t.Fatalf("cozumlenemedi: %v", err)
	}
	if e.UserAgent != `Mozilla "fake" Bot` {
		t.Errorf("istemci = %q", e.UserAgent)
	}
}

func TestNormalizePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "-"},
		{"/", "/"},
		{"/orders/8421", "/orders/:id"},
		{"/orders/8421/items/9", "/orders/:id/items/:id"},
		{"/u/550e8400-e29b-41d4-a716-446655440000/x", "/u/:id/x"},
		{"/f/9f1c2b3d4e5a6b7c8d9e0f1a2b3c4d5e/x", "/f/:id/x"},
		{"/search?q=merhaba&p=2", "/search"},
		{"/page#anchor", "/page"},
		{"http://ornek.com/a/b", "/a/b"},
		{"http://ornek.com", "/"},
		// Route names that merely look identifier-ish must survive, or a
		// genuinely popular endpoint disappears from the ranking.
		{"/api/v1/users", "/api/v1/users"},
		{"/deadbeef", "/deadbeef"},
	}
	for _, c := range cases {
		if got := NormalizePath(c.in); got != c.want {
			t.Errorf("NormalizePath(%q) = %q, beklenen %q", c.in, got, c.want)
		}
	}
}

func TestNormalizePath_UzunYolKesilir(t *testing.T) {
	got := NormalizePath("/" + strings.Repeat("a", 500))
	if len([]rune(got)) > maxPathLen+1 {
		t.Errorf("uzunluk = %d, sinir uygulanmadi", len([]rune(got)))
	}
}

func TestClientFamily(t *testing.T) {
	cases := []struct{ ua, want string }{
		{"", "Bilinmiyor"},
		{"curl/8.4.0", "curl"},
		{"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", "Googlebot"},
		{"Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36", "Chrome"},
		// Edge and Opera also claim Chrome and Safari; the most specific
		// claim has to win or every browser collapses into one row.
		{"Mozilla/5.0 ... Chrome/120.0 Safari/537.36 Edg/120.0", "Edge"},
		{"Mozilla/5.0 ... Chrome/120.0 Safari/537.36 OPR/106.0", "Opera"},
		{"Mozilla/5.0 (Macintosh) AppleWebKit/605.1 Version/17.0 Safari/605.1.15", "Safari"},
		{"Mozilla/5.0 (X11; Linux) Gecko/20100101 Firefox/121.0", "Firefox"},
		{"SomeUnknownAgent/1.0", "Diğer"},
	}
	for _, c := range cases {
		if got := ClientFamily(c.ua); got != c.want {
			t.Errorf("ClientFamily(%q) = %q, beklenen %q", c.ua, got, c.want)
		}
	}
}
