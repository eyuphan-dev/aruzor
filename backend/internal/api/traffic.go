package api

import (
	"context"
	"net/http"
	"time"

	"aruzor/internal/store"
	"aruzor/internal/traffic"
)

// The Traffic page answers "who is talking to this server and what are
// they getting back" — questions Prometheus exporters structurally cannot
// answer, because they publish counters about a process rather than the
// individual requests that reached it. Everything here is read from the web
// server's own access log by internal/traffic.
//
// Access-log data is more sensitive than the rest of Aruzor: it contains
// visitors' IP addresses, the URLs they asked for and the clients they
// used. So these endpoints sit behind admin+, not behind plain
// authentication like the metric queries do — a viewer account exists to
// look at dashboards, not at the browsing history of everyone who visited
// the site.

// trafficRanges maps the URL's range parameter to a window and the
// resolution the series is rolled up to. Longer windows are coarser on
// purpose: a week of per-minute points is 10,080 values, which is slower to
// send than to look at and far denser than any screen can draw.
var trafficRanges = map[string]struct {
	window time.Duration
	step   time.Duration
}{
	"1h":  {time.Hour, time.Minute},
	"6h":  {6 * time.Hour, 5 * time.Minute},
	"24h": {24 * time.Hour, 10 * time.Minute},
	"7d":  {7 * 24 * time.Hour, time.Hour},
}

const defaultTrafficRange = "24h"

// rankingLimit is how many rows each ranked panel returns. Ten is what fits
// in a panel without scrolling; the interesting entry in a "top talkers"
// list is essentially always in the first few.
const rankingLimit = 10

type trafficPoint struct {
	At       int64 `json:"at"` // unix seconds, the start of the step
	Requests int64 `json:"requests"`
	Bytes    int64 `json:"bytes"`
	S2xx     int64 `json:"s2xx"`
	S3xx     int64 `json:"s3xx"`
	S4xx     int64 `json:"s4xx"`
	S5xx     int64 `json:"s5xx"`
}

type trafficTotals struct {
	Requests      int64   `json:"requests"`
	Bytes         int64   `json:"bytes"`
	Errors5xx     int64   `json:"errors5xx"`
	Errors4xx     int64   `json:"errors4xx"`
	Unauthorized  int64   `json:"unauthorized"`
	RequestsPerS  float64 `json:"requestsPerSecond"`
	BytesPerS     float64 `json:"bytesPerSecond"`
	PeakRequests  float64 `json:"peakRequestsPerSecond"`
	ErrorRate     float64 `json:"errorRate"` // percent of requests that were 5xx
	AvgDurationMs float64 `json:"avgDurationMs"`
	HasDuration   bool    `json:"hasDuration"`
}

// trafficFields tells the UI which panels this log format can actually
// fill. A panel that renders empty because the operator's log_format is
// missing $host looks identical to a panel that is empty because nothing
// happened — so the difference is reported explicitly and the page shows
// the one-line config change instead of an empty chart.
type trafficFields struct {
	Host     bool `json:"host"`
	Service  bool `json:"service"`
	Duration bool `json:"duration"`
}

type trafficOverview struct {
	Enabled         bool                   `json:"enabled"`
	HasData         bool                   `json:"hasData"`
	ConfiguredPaths []string               `json:"configuredPaths"`
	Sources         []store.TrafficSource  `json:"sources"`
	Fields          trafficFields          `json:"fields"`
	Range           string                 `json:"range"`
	StepSeconds     int                    `json:"stepSeconds"`
	Totals          trafficTotals          `json:"totals"`
	Series          []trafficPoint         `json:"series"`
	TopIPs          []store.TrafficDim     `json:"topIps"`
	TopIPsByBytes   []store.TrafficDim     `json:"topIpsByBytes"`
	TopPaths        []store.TrafficDim     `json:"topPaths"`
	TopClients      []store.TrafficDim     `json:"topClients"`
	TopHosts        []store.TrafficDim     `json:"topHosts"`
	TopServices     []store.TrafficDim     `json:"topServices"`
	Nodes           []store.TrafficDim     `json:"nodes"`
	StatusCodes     []store.TrafficDim     `json:"statusCodes"`
	Methods         []store.TrafficDim     `json:"methods"`
	ErrorPaths      []store.TrafficDim     `json:"errorPaths"`
	Unauthorized    []store.TrafficRequest `json:"unauthorized"`
	Recent          []store.TrafficRequest `json:"recent"`
}

func (r *Router) handleTraffic(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	rangeKey := req.URL.Query().Get("range")
	spec, ok := trafficRanges[rangeKey]
	if !ok {
		rangeKey = defaultTrafficRange
		spec = trafficRanges[defaultTrafficRange]
	}
	since := time.Now().Add(-spec.window)

	out := trafficOverview{
		Enabled:         len(r.trafficPaths) > 0,
		ConfiguredPaths: r.trafficPaths,
		Range:           rangeKey,
		StepSeconds:     int(spec.step / time.Second),
		Sources:         []store.TrafficSource{},
	}
	if out.ConfiguredPaths == nil {
		out.ConfiguredPaths = []string{}
	}

	hasData, err := r.db.HasTrafficData(ctx)
	if err != nil {
		r.log.Error("trafik verisi kontrol edilemedi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	out.HasData = hasData

	if sources, err := r.db.ListTrafficSources(ctx); err != nil {
		r.log.Error("trafik kaynaklari alinamadi", "hata", err.Error())
	} else {
		out.Sources = sources
	}

	buckets, err := r.db.TrafficSeries(ctx, since)
	if err != nil {
		r.log.Error("trafik serisi alinamadi", "hata", err.Error())
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	out.Series = rollUp(buckets, spec.step)
	out.Totals = totalsFrom(buckets, spec.window, spec.step)

	// One helper per ranked panel, all failing soft: a single slow or
	// erroring ranking should leave the rest of the page working rather
	// than turning the whole request into a 500.
	rank := func(dim string, byBytes bool) []store.TrafficDim {
		rows, err := r.db.TrafficRanking(ctx, dim, since, byBytes, rankingLimit)
		if err != nil {
			r.log.Error("trafik siralamasi alinamadi", "boyut", dim, "hata", err.Error())
			return []store.TrafficDim{}
		}
		return rows
	}

	out.TopIPs = rank(traffic.DimIP, false)
	out.TopIPsByBytes = rank(traffic.DimIP, true)
	out.TopPaths = rank(traffic.DimPath, false)
	out.TopClients = rank(traffic.DimClient, false)
	out.TopHosts = rank(traffic.DimHost, false)
	out.TopServices = rank(traffic.DimService, false)
	out.Nodes = rank(traffic.DimNode, false)
	out.StatusCodes = rank(traffic.DimStatus, false)
	out.Methods = rank(traffic.DimMethod, false)

	if rows, err := r.db.TrafficErrorPaths(ctx, since, rankingLimit); err != nil {
		r.log.Error("hatali yollar alinamadi", "hata", err.Error())
		out.ErrorPaths = []store.TrafficDim{}
	} else {
		out.ErrorPaths = rows
	}

	out.Unauthorized = r.trafficRequests(ctx, store.TrafficUnauthorized, since, rankingLimit)
	out.Recent = r.trafficRequests(ctx, store.TrafficAll, since, 25)

	// Availability is read off the data rather than off configuration:
	// what matters to the reader is whether these panels have anything in
	// them for the window they are looking at, not what the log_format said
	// at some point in the past.
	out.Fields = trafficFields{
		Host:     len(out.TopHosts) > 0,
		Service:  len(out.TopServices) > 0,
		Duration: out.Totals.HasDuration,
	}

	writeJSON(w, http.StatusOK, out)
}

func (r *Router) trafficRequests(ctx context.Context, filter store.TrafficRequestFilter, since time.Time, limit int) []store.TrafficRequest {
	rows, err := r.db.ListTrafficRequests(ctx, filter, since, limit)
	if err != nil {
		r.log.Error("son istekler alinamadi", "filtre", string(filter), "hata", err.Error())
		return []store.TrafficRequest{}
	}
	return rows
}

// handleTrafficRequests backs the recent-requests table's own filter and
// "show more", which would otherwise mean re-running every ranking query
// just to page a list.
func (r *Router) handleTrafficRequests(w http.ResponseWriter, req *http.Request) {
	filter := store.TrafficAll
	switch req.URL.Query().Get("filter") {
	case string(store.TrafficUnauthorized):
		filter = store.TrafficUnauthorized
	case string(store.TrafficErrors):
		filter = store.TrafficErrors
	}
	// Scoped to the same window the rest of the page is showing, so the
	// filtered list cannot answer a different question from the panels
	// around it.
	spec, ok := trafficRanges[req.URL.Query().Get("range")]
	if !ok {
		spec = trafficRanges[defaultTrafficRange]
	}
	since := time.Now().Add(-spec.window)
	writeJSON(w, http.StatusOK, r.trafficRequests(req.Context(), filter, since, 100))
}

// rollUp collapses per-minute buckets into the step the requested range
// uses. Steps with no traffic are emitted as zeroes rather than skipped: a
// gap in a request-rate line reads as "the collector was down", and a quiet
// night is not that.
func rollUp(buckets []store.TrafficBucket, step time.Duration) []trafficPoint {
	if len(buckets) == 0 {
		return []trafficPoint{}
	}

	stepSecs := int64(step / time.Second)
	first := buckets[0].At.Unix() / stepSecs * stepSecs
	last := buckets[len(buckets)-1].At.Unix() / stepSecs * stepSecs

	index := map[int64]*trafficPoint{}
	out := make([]trafficPoint, 0, (last-first)/stepSecs+1)
	for at := first; at <= last; at += stepSecs {
		out = append(out, trafficPoint{At: at})
	}
	for i := range out {
		index[out[i].At] = &out[i]
	}

	for _, b := range buckets {
		p := index[b.At.Unix()/stepSecs*stepSecs]
		if p == nil {
			continue
		}
		p.Requests += b.Requests
		p.Bytes += b.Bytes
		p.S2xx += b.S2xx
		p.S3xx += b.S3xx
		p.S4xx += b.S4xx
		p.S5xx += b.S5xx
	}
	return out
}

// totalsFrom reduces the window to the headline numbers.
//
// Rates are averaged over the whole requested window rather than over the
// minutes that happen to have rows, so "requests per second over the last
// 24 hours" means what it says even when the server was idle for twenty of
// them. The peak is computed per step instead, since that is the question
// it answers: how busy did it get at its busiest.
func totalsFrom(buckets []store.TrafficBucket, window, step time.Duration) trafficTotals {
	var t trafficTotals
	var durationSum, durationCount int64
	var peakPerStep int64

	stepSecs := int64(step / time.Second)
	perStep := map[int64]int64{}

	for _, b := range buckets {
		t.Requests += b.Requests
		t.Bytes += b.Bytes
		t.Errors5xx += b.S5xx
		t.Errors4xx += b.S4xx
		t.Unauthorized += b.Unauthorized
		durationSum += b.DurationMsSum
		durationCount += b.DurationCount

		slot := b.At.Unix() / stepSecs * stepSecs
		perStep[slot] += b.Requests
		if perStep[slot] > peakPerStep {
			peakPerStep = perStep[slot]
		}
	}

	seconds := window.Seconds()
	if seconds > 0 {
		t.RequestsPerS = float64(t.Requests) / seconds
		t.BytesPerS = float64(t.Bytes) / seconds
	}
	if stepSecs > 0 {
		t.PeakRequests = float64(peakPerStep) / float64(stepSecs)
	}
	if t.Requests > 0 {
		t.ErrorRate = float64(t.Errors5xx) / float64(t.Requests) * 100
	}
	if durationCount > 0 {
		t.AvgDurationMs = float64(durationSum) / float64(durationCount)
		t.HasDuration = true
	}
	return t
}
