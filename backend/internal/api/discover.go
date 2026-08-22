package api

import (
	"net/http"
	"sync"
	"time"

	"aruzor/internal/discovery"
)

// Discovery walks every metric name Prometheus knows, which on a large
// install is a heavy answer to compute repeatedly. The result only changes
// when someone adds or removes an exporter, so a short cache keeps the
// setup screen responsive without ever serving a stale-feeling answer.
const discoveryCacheTTL = 60 * time.Second

type discoveryCache struct {
	mu        sync.Mutex
	result    discovery.Result
	fetchedAt time.Time
}

var discoverCache discoveryCache

// handleDiscover reports which exporters this Prometheus is scraping and
// what panels they make possible. It is what lets a fresh install populate
// itself instead of opening on an empty dashboard.
func (r *Router) handleDiscover(w http.ResponseWriter, req *http.Request) {
	prom, err := r.resolveDatasource(req.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	// Only the default datasource is cached: a request naming an explicit
	// datasource is rare and pointing at a different Prometheus entirely.
	useCache := req.URL.Query().Get("datasource") == ""

	if useCache {
		discoverCache.mu.Lock()
		if time.Since(discoverCache.fetchedAt) < discoveryCacheTTL {
			cached := discoverCache.result
			discoverCache.mu.Unlock()
			writeJSON(w, http.StatusOK, cached)
			return
		}
		discoverCache.mu.Unlock()
	}

	result := discovery.Run(req.Context(), prom)

	if useCache && result.PrometheusReachable {
		// Only a successful probe is cached. Caching a failure would keep the
		// setup screen showing "unreachable" for a minute after Prometheus
		// comes up, which is exactly when someone is watching it.
		discoverCache.mu.Lock()
		discoverCache.result = result
		discoverCache.fetchedAt = time.Now()
		discoverCache.mu.Unlock()
	}

	writeJSON(w, http.StatusOK, result)
}
