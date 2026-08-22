package uptime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"
)

// Failure classes. These are recorded, not guessed: each one is decided by
// what the network stack actually reported, never by inference. The UI
// turns a class into a list of things worth checking; that list is advice
// and is presented as advice. The class itself is a fact.
const (
	ClassOK              = ""
	ClassDNS             = "dns"              // name did not resolve
	ClassRefused         = "refused"          // host reachable, nothing listening
	ClassTimeoutConnect  = "timeout_connect"  // no answer to the connection attempt at all
	ClassTimeoutResponse = "timeout_response" // connected, then the application never replied
	ClassTLSCert         = "tls_cert"         // certificate expired / wrong name / untrusted
	ClassTLSHandshake    = "tls_handshake"    // TLS itself failed for another reason
	ClassHTTPServerError = "http_server"      // 5xx
	ClassHTTPBlocked     = "http_blocked"     // 403 / 429
	ClassHTTPStatus      = "http_status"      // some other unexpected status
	ClassUnknown         = "unknown"
)

// userAgent identifies the monitor to the site it is checking. Go sends
// "Go-http-client/1.1" by default, which is a common thing for bot
// protection to throttle, and it tells an operator reading their access log
// nothing about who is knocking every minute. Saying plainly what this is
// costs one header.
const userAgent = "Aruzor-Monitor/1.0 (+https://github.com/aruzor)"

// probeClient is dedicated rather than http.DefaultClient, so a timeout or
// a transport setting here can never leak into unrelated requests made
// elsewhere in the process.
var probeClient = &http.Client{
	Transport: &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     30 * time.Second,
	},
}

// probeResult is one check. Phase timings are the useful part: knowing
// which layer the time went into separates a network problem from a TLS
// problem from an application problem without guessing at any of them.
type probeResult struct {
	ok          bool
	latencyMs   int
	errorClass  string
	errorDetail string
	connectMs   *int
	tlsMs       *int
	certExpiry  *time.Time
	statusCode  int
	// attempts is how many tries this check needed. Anything above one
	// means the service wobbled: the app shows that, and the chat does not.
	attempts int
}

// probeAttempts is how many times a check is tried before it counts as a
// failure, and howLongBetween is the pause in between.
//
// A single failed request is not evidence that a site is down. A browser
// retries a dropped connection without telling anyone, and so does the
// person pressing enter again — which is exactly why a monitor that gives
// up after one attempt reports outages nobody else can see. Retrying is
// what makes "no response" mean the same thing to us as it does to a
// visitor.
const (
	probeAttempts    = 3
	attemptGap       = 2 * time.Second
	attemptTimeout   = 10 * time.Second
	totalProbeBudget = probeAttempts*attemptTimeout + probeAttempts*attemptGap
)

// probe runs one check, retrying a failure before believing it. The first
// success wins and carries its own timings; if every attempt fails, the
// last failure is what gets recorded, since it is the freshest evidence.
func probe(ctx context.Context, kind, target string) probeResult {
	var last probeResult
	for attempt := 1; attempt <= probeAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		var res probeResult
		if kind == "tcp" {
			res = probeTCP(attemptCtx, target)
		} else {
			res = probeHTTP(attemptCtx, target)
		}
		cancel()

		res.attempts = attempt
		if res.ok {
			return res
		}
		last = res

		// A blocked request is a decision the far end already made; asking
		// again immediately is more likely to deepen a rate limit than to
		// get a different answer.
		if res.errorClass == ClassHTTPBlocked {
			return res
		}
		if attempt < probeAttempts {
			select {
			case <-ctx.Done():
				return last
			case <-time.After(attemptGap):
			}
		}
	}
	return last
}

func probeHTTP(ctx context.Context, target string) probeResult {
	var (
		connectStart, tlsStart time.Time
		connectMs, tlsMs       *int
		certExpiry             *time.Time
	)

	trace := &httptrace.ClientTrace{
		ConnectStart: func(_, _ string) { connectStart = time.Now() },
		ConnectDone: func(_, _ string, err error) {
			if err == nil && !connectStart.IsZero() {
				ms := int(time.Since(connectStart).Milliseconds())
				connectMs = &ms
			}
		},
		TLSHandshakeStart: func() { tlsStart = time.Now() },
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			if err != nil {
				return
			}
			if !tlsStart.IsZero() {
				ms := int(time.Since(tlsStart).Milliseconds())
				tlsMs = &ms
			}
			// The server hands us its certificate during the handshake, so
			// the expiry date is already in hand on every successful check.
			// Knowing it 14 days early is worth more than diagnosing it the
			// morning it takes the site down.
			if len(state.PeerCertificates) > 0 {
				expiry := state.PeerCertificates[0].NotAfter
				certExpiry = &expiry
			}
		},
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, target, nil)
	if err != nil {
		return probeResult{errorClass: ClassUnknown, errorDetail: err.Error()}
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := probeClient.Do(req)
	latencyMs := int(time.Since(start).Milliseconds())

	if err != nil {
		class, detail := classifyError(err, connectMs != nil)
		return probeResult{
			latencyMs: latencyMs, errorClass: class, errorDetail: detail,
			connectMs: connectMs, tlsMs: tlsMs, certExpiry: certExpiry,
		}
	}
	defer resp.Body.Close()

	res := probeResult{
		latencyMs: latencyMs, connectMs: connectMs, tlsMs: tlsMs,
		certExpiry: certExpiry, statusCode: resp.StatusCode,
	}
	res.ok, res.errorClass, res.errorDetail = classifyStatus(resp.StatusCode)
	return res
}

func probeTCP(ctx context.Context, target string) probeResult {
	start := time.Now()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", target)
	latencyMs := int(time.Since(start).Milliseconds())
	if err != nil {
		class, detail := classifyError(err, false)
		return probeResult{latencyMs: latencyMs, errorClass: class, errorDetail: detail}
	}
	conn.Close()
	return probeResult{ok: true, latencyMs: latencyMs, connectMs: &latencyMs}
}

// classifyError maps a transport error onto a class. connected says whether
// the TCP connection was established before the failure, which is what
// separates "nothing answered at all" from "it answered and then went
// quiet" — two problems with nothing in common.
func classifyError(err error, connected bool) (class, detail string) {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return ClassDNS, dnsErr.Err
	}

	var certErr x509.CertificateInvalidError
	if errors.As(err, &certErr) {
		if certErr.Reason == x509.Expired {
			return ClassTLSCert, "sertifikanın süresi dolmuş"
		}
		return ClassTLSCert, certErr.Error()
	}
	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return ClassTLSCert, "sertifika bu alan adı için geçerli değil"
	}
	var authErr x509.UnknownAuthorityError
	if errors.As(err, &authErr) {
		return ClassTLSCert, "sertifikayı doğrulayan kök otorite tanınmıyor"
	}
	var recErr tls.RecordHeaderError
	if errors.As(err, &recErr) {
		return ClassTLSHandshake, recErr.Msg
	}

	text := err.Error()
	if strings.Contains(text, "connection refused") {
		return ClassRefused, "bağlantı reddedildi"
	}
	if strings.Contains(text, "tls:") || strings.Contains(text, "handshake") {
		return ClassTLSHandshake, trimError(text)
	}

	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		if connected {
			return ClassTimeoutResponse, "bağlantı kuruldu, yanıt gelmedi"
		}
		return ClassTimeoutConnect, "bağlantı kurulamadı"
	}

	return ClassUnknown, trimError(text)
}

// classifyStatus decides whether a status code counts as up. The range is
// unchanged from before this file existed; what is new is naming why a code
// outside it failed, since 503 and 403 send you to completely different
// places.
func classifyStatus(code int) (ok bool, class, detail string) {
	switch {
	case code >= 200 && code < 400:
		return true, ClassOK, ""
	case code == 403 || code == 429:
		return false, ClassHTTPBlocked, fmt.Sprintf("istek engellendi (HTTP %d)", code)
	case code >= 500:
		return false, ClassHTTPServerError, fmt.Sprintf("sunucu hata döndü (HTTP %d)", code)
	default:
		return false, ClassHTTPStatus, fmt.Sprintf("beklenmeyen yanıt (HTTP %d)", code)
	}
}

// trimError keeps a raw transport error short enough for a Telegram line
// and a table cell without losing the part that identifies it.
func trimError(text string) string {
	if i := strings.LastIndex(text, ": "); i >= 0 && i+2 < len(text) {
		text = text[i+2:]
	}
	if len(text) > 120 {
		text = text[:120]
	}
	return text
}

// causeLabel is the one line the chat gets: what actually happened, in
// plain words. Deliberately short and deliberately free of advice — the
// list of things to go and check belongs in the app, where there is room
// to lay it out and where nobody is reading it on a lock screen.
func causeLabel(class, detail string) string {
	switch class {
	case ClassDNS:
		return "alan adı çözülemedi"
	case ClassRefused:
		return "bağlantı reddedildi — o portta dinleyen bir servis yok"
	case ClassTimeoutConnect:
		return "bağlantı kurulamadı, zaman aşımı"
	case ClassTimeoutResponse:
		return "bağlantı kuruldu ama yanıt gelmedi, zaman aşımı"
	case ClassTLSCert:
		if detail != "" {
			return "sertifika hatası — " + detail
		}
		return "sertifika hatası"
	case ClassTLSHandshake:
		return "TLS el sıkışması başarısız"
	case ClassHTTPServerError, ClassHTTPBlocked, ClassHTTPStatus:
		return detail
	}
	if detail != "" {
		return detail
	}
	return "sebep belirlenemedi"
}
