package uptime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The reason this package retries at all: a single failed request is not
// evidence that a site is down. A browser retries a dropped connection
// without telling anyone, so a monitor that gives up after one attempt
// reports outages the person looking at the site cannot reproduce — which
// is exactly the complaint that produced this code.
func TestProbeRetriesBeforeCallingItDown(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fails twice, then answers — the shape of a brief stall.
		if atomic.AddInt32(&hits, 1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := probe(context.Background(), "http", srv.URL, httpCheckConfig{})
	if !res.ok {
		t.Fatalf("ucuncu denemede basarili olmaliydi, sinif=%q", res.errorClass)
	}
	if res.attempts != 3 {
		t.Errorf("deneme sayisi = %d, beklenen 3", res.attempts)
	}
}

// A site that is genuinely broken must still be reported, and the recorded
// reason has to be the real one rather than something the retry loop made up.
func TestProbeReportsFailureAfterEveryAttempt(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	res := probe(context.Background(), "http", srv.URL, httpCheckConfig{})
	if res.ok {
		t.Fatal("surekli 503 donen bir site basarili sayilmamali")
	}
	if res.attempts != probeAttempts {
		t.Errorf("deneme sayisi = %d, beklenen %d", res.attempts, probeAttempts)
	}
	if got := atomic.LoadInt32(&hits); got != int32(probeAttempts) {
		t.Errorf("sunucuya %d istek gitti, beklenen %d", got, probeAttempts)
	}
	if res.errorClass != ClassHTTPServerError {
		t.Errorf("sinif = %q, beklenen %q", res.errorClass, ClassHTTPServerError)
	}
}

// A healthy site must not pay for the retry machinery.
func TestProbeStopsAtFirstSuccess(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	start := time.Now()
	res := probe(context.Background(), "http", srv.URL, httpCheckConfig{})
	if !res.ok || res.attempts != 1 {
		t.Fatalf("ok=%v deneme=%d, beklenen ok=true deneme=1", res.ok, res.attempts)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("sunucuya %d istek gitti, beklenen 1", got)
	}
	if elapsed := time.Since(start); elapsed > attemptGap {
		t.Errorf("saglikli kontrol %v surdu, bekleme yapilmamaliydi", elapsed)
	}
}

// Being blocked is a decision the far end already made. Asking again
// immediately is more likely to deepen a rate limit than to change it.
func TestProbeDoesNotRetryWhenBlocked(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	res := probe(context.Background(), "http", srv.URL, httpCheckConfig{})
	if res.errorClass != ClassHTTPBlocked {
		t.Fatalf("sinif = %q, beklenen %q", res.errorClass, ClassHTTPBlocked)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("sunucuya %d istek gitti, beklenen 1", got)
	}
}

// The monitor should say who it is. "Go-http-client/1.1" tells an operator
// reading their access log nothing, and is a common thing for bot
// protection to throttle.
func TestProbeIdentifiesItself(t *testing.T) {
	seen := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case seen <- r.UserAgent():
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	probe(context.Background(), "http", srv.URL, httpCheckConfig{})
	if got := <-seen; got != userAgent {
		t.Errorf("User-Agent = %q, beklenen %q", got, userAgent)
	}
}

// An expected list replaces the default range rather than extending it —
// "expect 404" on a check deliberately probing a route that should not
// exist has to be expressible.
func TestClassifyStatus_BeklenenListeIle(t *testing.T) {
	ok, class, _ := classifyStatus(200, []int{200, 302})
	if !ok || class != ClassOK {
		t.Fatalf("200 beklenen listede olmali: ok=%v sinif=%q", ok, class)
	}
	ok, class, _ = classifyStatus(302, []int{200, 302})
	if !ok || class != ClassOK {
		t.Fatalf("302 beklenen listede olmali: ok=%v sinif=%q", ok, class)
	}
	ok, class, detail := classifyStatus(404, []int{200, 302})
	if ok || class != ClassHTTPStatus {
		t.Fatalf("404 beklenen listede olmamali: ok=%v sinif=%q", ok, class)
	}
	if !strings.Contains(detail, "beklenen: 200,302") || !strings.Contains(detail, "gelen: 404") {
		t.Errorf("detay mesaji = %q, beklenen ve gelen kodlari icermeli", detail)
	}
	// Deliberately probing a route that should be gone: expecting 404
	// itself must count as healthy.
	ok, _, _ = classifyStatus(404, []int{404})
	if !ok {
		t.Error("expected=[404] iken 404 saglikli sayilmali")
	}
}

// The request itself must carry the configured method and body, and fall
// back to a sensible default Content-Type when none was given.
func TestProbeHTTP_OzelMetodVeGovde(t *testing.T) {
	var gotMethod, gotBody, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := probe(context.Background(), "http", srv.URL, httpCheckConfig{
		method: "POST",
		body:   `{"user":"test"}`,
	})
	if !res.ok {
		t.Fatalf("kontrol basarisiz oldu, sinif=%q", res.errorClass)
	}
	if gotMethod != "POST" {
		t.Errorf("metod = %q, beklenen POST", gotMethod)
	}
	if gotBody != `{"user":"test"}` {
		t.Errorf("govde = %q, beklenen govde gonderilen ile ayni olmali", gotBody)
	}
	if gotContentType != "application/json" {
		t.Errorf("content-type = %q, beklenen varsayilan application/json", gotContentType)
	}
}

// An explicitly configured Content-Type must override the default.
func TestProbeHTTP_ContentTypeOverride(t *testing.T) {
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	probe(context.Background(), "http", srv.URL, httpCheckConfig{
		method:      "POST",
		body:        "a=1&b=2",
		contentType: "application/x-www-form-urlencoded",
	})
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Errorf("content-type = %q, beklenen override edilen deger", gotContentType)
	}
}

// Status ok, but the expected marker text is missing from the body — a
// genuinely new class of fact, not a network failure.
func TestProbeHTTP_IcerikEslesmedi(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html>Bakim modundayiz</html>"))
	}))
	defer srv.Close()

	res := probe(context.Background(), "http", srv.URL, httpCheckConfig{expectBodyContains: "Hos geldiniz"})
	if res.ok {
		t.Fatal("beklenen metin yokken saglikli sayilmamali")
	}
	if res.errorClass != ClassContentMismatch {
		t.Errorf("sinif = %q, beklenen %q", res.errorClass, ClassContentMismatch)
	}
}

func TestProbeHTTP_IcerikEslesti(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html>Hos geldiniz, panel</html>"))
	}))
	defer srv.Close()

	res := probe(context.Background(), "http", srv.URL, httpCheckConfig{expectBodyContains: "Hos geldiniz"})
	if !res.ok {
		t.Fatalf("beklenen metin varken saglikli sayilmali, sinif=%q", res.errorClass)
	}
}

// A 500 with the right words in its error page is still a 500 — the body
// must never be consulted once the status itself has already failed.
func TestProbeHTTP_DurumBasarisizsaIcerikOkunmaz(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Hos geldiniz"))
	}))
	defer srv.Close()

	res := probe(context.Background(), "http", srv.URL, httpCheckConfig{expectBodyContains: "Hos geldiniz"})
	if res.errorClass != ClassHTTPServerError {
		t.Errorf("sinif = %q, beklenen %q (icerik kontrolu duruma bakmadan calismamali)", res.errorClass, ClassHTTPServerError)
	}
}

// An unconfigured check must not pay to read a body it will never look at —
// the existing bandwidth-conscious behavior this feature must not regress.
func TestProbeHTTP_DogrulamaYokkenGovdeOkunmaz(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("herhangi bir icerik"))
	}))
	defer srv.Close()

	res := probe(context.Background(), "http", srv.URL, httpCheckConfig{})
	if !res.ok {
		t.Fatal("saglikli bir yanit basarisiz sayildi")
	}
	// Indirect check: probeHTTP only reads/compares the body when
	// expectBodyContains is set, so the class must never become
	// content_mismatch for any response when the field is empty.
	if res.errorClass == ClassContentMismatch {
		t.Error("dogrulama tanimlanmamisken content_mismatch olusmamali")
	}
}

// The response body read for a content assertion is capped — a huge body
// must not be buffered without bound.
func TestProbeHTTP_GovdeOkumaSiniri(t *testing.T) {
	big := strings.Repeat("x", maxBodyReadBytes+1000) + "IMZA"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(big))
	}))
	defer srv.Close()

	// The marker sits past the read cap, so it must not be found — proves
	// the read is actually bounded rather than silently reading everything.
	res := probe(context.Background(), "http", srv.URL, httpCheckConfig{expectBodyContains: "IMZA"})
	if res.ok {
		t.Fatal("sinirin otesindeki metin bulunmamali, okuma sinirsiz olmus olabilir")
	}
	if res.errorClass != ClassContentMismatch {
		t.Errorf("sinif = %q, beklenen %q", res.errorClass, ClassContentMismatch)
	}
}
