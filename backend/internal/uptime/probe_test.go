package uptime

import (
	"context"
	"net/http"
	"net/http/httptest"
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

	res := probe(context.Background(), "http", srv.URL)
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

	res := probe(context.Background(), "http", srv.URL)
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
	res := probe(context.Background(), "http", srv.URL)
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

	res := probe(context.Background(), "http", srv.URL)
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

	probe(context.Background(), "http", srv.URL)
	if got := <-seen; got != userAgent {
		t.Errorf("User-Agent = %q, beklenen %q", got, userAgent)
	}
}
