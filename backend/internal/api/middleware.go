package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// withSecurityHeaders sets baseline hardening headers. Aruzor never renders
// third-party HTML, so a strict, no-external-source CSP is safe here.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'none'")
		next.ServeHTTP(w, req)
	})
}

const maxRequestBodyBytes = 1 << 20 // 1 MiB — every Aruzor request body is small JSON

func withBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req.Body = http.MaxBytesReader(w, req.Body, maxRequestBodyBytes)
		next.ServeHTTP(w, req)
	})
}

// loginLimiter blocks brute-force credential guessing: a fixed-window cap
// per source IP, checked only on the login endpoint. State is kept in
// memory, which is fine for the single-process deployment Aruzor targets.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	max      int
	window   time.Duration
	calls    int
}

func newLoginLimiter(max int, window time.Duration) *loginLimiter {
	return &loginLimiter{attempts: make(map[string][]time.Time), max: max, window: window}
}

func (l *loginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)
	var kept []time.Time
	for _, t := range l.attempts[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.attempts[key] = kept
		return false
	}
	l.attempts[key] = append(kept, now)

	// Every so often, sweep out keys whose attempts have all aged out —
	// otherwise every distinct source IP that ever tried to log in stays
	// in the map forever, a slow unbounded leak on a long-running process.
	l.calls++
	if l.calls >= 1000 {
		l.calls = 0
		for k, times := range l.attempts {
			stillRecent := false
			for _, t := range times {
				if t.After(cutoff) {
					stillRecent = true
					break
				}
			}
			if !stillRecent {
				delete(l.attempts, k)
			}
		}
	}

	return true
}

// clientIP intentionally ignores X-Forwarded-For: it's attacker-controlled
// unless a trusted reverse proxy strips and resets it, which Aruzor cannot
// assume. Trusting it here would let an attacker bypass the login rate
// limit just by sending a different header value on every request.
//
// RemoteAddr includes the ephemeral source port, which is different on
// every TCP connection — keying the rate limiter on the raw value would
// mean every request from the same client looks like a new source. Only
// the host part identifies the client.
func clientIP(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}
