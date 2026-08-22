package prometheus

import (
	"context"
	"log/slog"
	"time"
)

// candidateURLs are the places a Prometheus ends up on a machine where
// nobody has told Aruzor where to look: the two loopback spellings for a
// bare-metal install, the compose service name when Aruzor runs beside it
// in Docker, and the host gateway when Aruzor is containerised but
// Prometheus is not.
var candidateURLs = []string{
	"http://localhost:9090",
	"http://127.0.0.1:9090",
	"http://prometheus:9090",
	"http://host.docker.internal:9090",
}

// Detect resolves the Prometheus address to use. An explicit configuration
// always wins and is never probed away — a wrong explicit address should
// surface as a visible connection error, not be silently replaced by some
// other Prometheus that happens to be reachable.
//
// With nothing configured, each candidate is probed and the first that
// answers is used. This is what lets the same build run unchanged on a
// bare server, inside compose, and in a container next to a host-installed
// Prometheus.
func Detect(ctx context.Context, explicit string, log *slog.Logger) string {
	if explicit != "" {
		return explicit
	}

	for _, url := range candidateURLs {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		reachable := NewClient(url).Reachable(probeCtx)
		cancel()
		if reachable {
			log.Info("prometheus otomatik bulundu", "adres", url)
			return url
		}
	}

	// Nothing answered. Fall back to the most likely address rather than
	// failing to boot: Prometheus may simply be starting up alongside us,
	// and the UI reports the connection state anyway.
	log.Warn("prometheus bulunamadi, varsayilan adres kullanilacak",
		"adres", candidateURLs[0],
		"ipucu", "ARUZOR_PROMETHEUS_URL ile acikca belirtebilirsiniz")
	return candidateURLs[0]
}

// Reachable reports whether this Prometheus answers a trivial query. Used
// both by address detection at startup and by the connection indicator in
// the UI.
func (c *Client) Reachable(ctx context.Context) bool {
	_, err := c.Query(ctx, "1", time.Time{})
	return err == nil
}
