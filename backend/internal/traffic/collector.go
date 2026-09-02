package traffic

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"aruzor/internal/store"
)

// Dimension names. These are the grouping keys the Traffic page ranks by,
// and they are written into the database, so they are constants rather than
// literals scattered across the collector and the API.
const (
	DimIP      = "ip"
	DimPath    = "path"
	DimClient  = "ua"
	DimHost    = "host"
	DimService = "service"
	DimNode    = "node"
	DimStatus  = "status"
	DimMethod  = "method"
)

const (
	// How often the tailers are read and the window is written out. A minute
	// matches the bucket size, so a flush normally writes one bucket row.
	flushInterval = time.Minute

	// Per-minute top-N caps. Unbounded dimensions get a tight cap because
	// that cap is the only thing standing between a path-fuzzing scanner and
	// unbounded database growth. The closed dimensions — status codes, HTTP
	// methods, the handful of log files being followed — are capped far
	// higher because their cardinality is a property of the protocol, not of
	// whoever is sending traffic.
	openDimTopN   = 25
	closedDimTopN = 200

	// How many individual requests are kept from each flush, and in total.
	// Both are small on purpose: this table only ever backs a "recent
	// requests" list nobody scrolls more than a screen or two of.
	requestsPerFlush = 150
	requestsKept     = 1500

	// How long the per-minute aggregates are kept. The page offers ranges up
	// to 7 days, so this is what makes the longest of them answerable.
	retention = 7 * 24 * time.Hour
)

// Collector follows a set of access-log files and turns them into the
// bounded aggregates the Traffic page reads.
//
// It is a single goroutine that owns all of its state: nothing else touches
// the tailers or the in-flight window, so no locking is needed anywhere in
// this file. Everything leaves through one transactional write per flush.
type Collector struct {
	db       *store.Store
	log      *slog.Logger
	patterns []configuredPath

	tailers map[string]*tailer
}

// configuredPath is one entry from ARUZOR_ACCESS_LOG_PATHS: a filesystem
// glob plus an optional operator-chosen name. A glob rather than a plain
// path because per-site logging (one file per virtual host, which is what
// every hosting panel sets up) is the case where these panels are most
// useful, and enumerating those files by hand would mean editing config
// every time a site is added.
type configuredPath struct {
	name string // empty means "name each matched file after itself"
	glob string
}

// NewCollector parses the configured log paths. A nil Collector is returned
// when nothing is configured and no well-known log file exists, which is
// how the rest of the system asks "is this feature on".
func NewCollector(db *store.Store, log *slog.Logger, raw string) *Collector {
	patterns := parsePaths(raw)
	if len(patterns) == 0 {
		patterns = detectWellKnownLogs()
	}
	if len(patterns) == 0 {
		return nil
	}
	return &Collector{db: db, log: log, patterns: patterns, tailers: map[string]*tailer{}}
}

// parsePaths reads the "name=/glob,/other/glob" form. The name is optional
// per entry, so the simple case stays a bare path list.
func parsePaths(raw string) []configuredPath {
	var out []configuredPath
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		entry := configuredPath{glob: part}
		// Split on the first "=" only: a Windows-style path can't appear
		// here, but a filename certainly can contain more than one "=".
		if i := strings.Index(part, "="); i > 0 {
			entry.name = strings.TrimSpace(part[:i])
			entry.glob = strings.TrimSpace(part[i+1:])
		}
		if entry.glob != "" {
			out = append(out, entry)
		}
	}
	return out
}

// wellKnownLogs are the two places an access log is on a machine where
// nobody configured one: a stock nginx/apache install, and the layout every
// mainstream Turkish hosting panel uses. Picking these up automatically is
// the difference between the Traffic page working on first boot and it
// being a feature nobody discovers.
var wellKnownLogs = []string{
	"/var/log/nginx/access.log",
	"/www/wwwlogs/access.log",
	"/var/log/apache2/access.log",
}

func detectWellKnownLogs() []configuredPath {
	var out []configuredPath
	for _, path := range wellKnownLogs {
		// Readable, not merely present: these files are usually owned by the
		// web server's user, and a path Aruzor cannot open would register a
		// source that never produces a single line.
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		f.Close()
		out = append(out, configuredPath{glob: path})
	}
	return out
}

// Sources reports the configured globs, for the setup panel to show when
// nothing has been ingested yet.
func (c *Collector) Sources() []string {
	out := make([]string, 0, len(c.patterns))
	for _, p := range c.patterns {
		out = append(out, p.glob)
	}
	return out
}

// Run reads, aggregates and writes until the context is cancelled. The
// first pass happens immediately rather than after a full interval, so a
// restart does not leave a minute-wide hole in the charts.
func (c *Collector) Run(ctx context.Context) {
	c.restoreOffsets(ctx)
	c.log.Info("trafik toplayici baslatildi", "kaynaklar", strings.Join(c.Sources(), ", "))

	c.tick(ctx)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	prune := time.NewTicker(time.Hour)
	defer prune.Stop()

	for {
		select {
		case <-ctx.Done():
			// One last read so requests served in the final partial minute
			// are not lost to a restart.
			c.tick(context.WithoutCancel(ctx))
			return
		case <-ticker.C:
			c.tick(ctx)
		case <-prune.C:
			if err := c.db.PruneTraffic(ctx, time.Now().Add(-retention), requestsKept); err != nil {
				c.log.Error("eski trafik verisi temizlenemedi", "hata", err.Error())
			}
		}
	}
}

// restoreOffsets picks reading back up where the previous process stopped.
// Without it every restart would re-ingest the backfill window and double
// count everything in it.
func (c *Collector) restoreOffsets(ctx context.Context) {
	saved, err := c.db.ListTrafficSources(ctx)
	if err != nil {
		c.log.Error("trafik kaynaklari okunamadi", "hata", err.Error())
		return
	}
	for _, s := range saved {
		c.tailers[s.ID] = &tailer{
			id: s.ID, name: s.Name, path: s.Path,
			offset: s.ReadOffset, lines: s.Lines, unparsed: s.Unparsed,
		}
	}
}

func (c *Collector) tick(ctx context.Context) {
	c.discover()

	w := newWindow()
	now := time.Now()
	var touched []store.TrafficSource

	for _, t := range c.tailers {
		before := t.lines
		if err := t.read(func(e Entry) { w.add(t.name, e) }); err != nil {
			// A log file that was deleted, or that Aruzor has no permission
			// to read, must not stop the other sources — and must not be
			// reported again every minute for as long as it stays that way.
			if t.lastErr != err.Error() {
				t.lastErr = err.Error()
				c.log.Warn("erisim logu okunamadi", "dosya", t.path, "hata", t.lastErr)
			}
			continue
		}
		if t.lastErr != "" {
			c.log.Info("erisim logu tekrar okunabiliyor", "dosya", t.path)
			t.lastErr = ""
		}
		if t.lines == before && t.offset == 0 {
			continue // nothing read and nothing to remember yet
		}
		readAt := now
		touched = append(touched, store.TrafficSource{
			ID: t.id, Name: t.name, Path: t.path,
			ReadOffset: t.offset, Lines: t.lines, Unparsed: t.unparsed,
			LastReadAt: &readAt,
		})
	}

	flush := w.flush()
	flush.Sources = touched
	if len(flush.Buckets) == 0 && len(flush.Sources) == 0 {
		return
	}
	if err := c.db.SaveTrafficFlush(ctx, flush); err != nil {
		c.log.Error("trafik verisi kaydedilemedi", "hata", err.Error())
	}
}

// discover re-expands the configured globs on every tick so a virtual host
// added after Aruzor started is picked up without a restart.
func (c *Collector) discover() {
	for _, p := range c.patterns {
		matches, err := filepath.Glob(p.glob)
		if err != nil {
			c.log.Warn("erisim logu deseni cozumlenemedi", "desen", p.glob, "hata", err.Error())
			continue
		}
		for _, path := range matches {
			// A glob over a log directory also matches error logs and
			// rotated archives. Those parse as zero valid lines, so leaving
			// them in would only pad the "unparsed" count with noise the
			// operator can do nothing about.
			if !looksLikeAccessLog(path) {
				continue
			}
			id := sourceID(path)
			if _, exists := c.tailers[id]; exists {
				continue
			}
			c.tailers[id] = &tailer{id: id, name: sourceName(p.name, path), path: path}
		}
	}
}

// looksLikeAccessLog excludes the files that sit next to an access log and
// are definitely not one. It is a filename test rather than a content test
// on purpose: a content sniff would have to read a rotated multi-gigabyte
// archive to decide to ignore it.
func looksLikeAccessLog(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(base, "error"):
		return false
	case strings.HasSuffix(base, ".gz"), strings.HasSuffix(base, ".zip"), strings.HasSuffix(base, ".bz2"):
		return false // rotated archive: history, already counted when it was live
	}
	// A rotated-but-uncompressed file (access.log.1) is the same history.
	if i := strings.LastIndex(base, "."); i > 0 && isAllDigits(base[i+1:]) {
		return false
	}
	return true
}

// sourceID is derived from the path so a source keeps its stored read
// offset across restarts, and so a renamed or moved log starts cleanly
// rather than resuming at an offset that means nothing in the new file.
func sourceID(path string) string {
	sum := sha1.Sum([]byte(path))
	return hex.EncodeToString(sum[:8])
}

// sourceName is what appears in the node panel. A per-site log is named
// after the site, which is exactly the label that panel wants; a single
// shared access.log falls back to its own filename.
func sourceName(configured, path string) string {
	if configured != "" {
		return configured
	}
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".log")
	base = strings.TrimSuffix(base, ".access")
	if base == "" {
		return path
	}
	return base
}

// window is one flush worth of accumulation, held in memory and thrown away
// once written.
type window struct {
	buckets map[int64]*store.TrafficBucket
	dims    map[dimKey]*store.TrafficDim
	recent  []store.TrafficRequest
}

type dimKey struct {
	minute int64
	dim    string
	key    string
}

func newWindow() *window {
	return &window{
		buckets: map[int64]*store.TrafficBucket{},
		dims:    map[dimKey]*store.TrafficDim{},
	}
}

func (w *window) add(source string, e Entry) {
	minute := e.At.Truncate(time.Minute)
	unix := minute.Unix()

	b := w.buckets[unix]
	if b == nil {
		b = &store.TrafficBucket{At: minute}
		w.buckets[unix] = b
	}
	b.Requests++
	b.Bytes += e.Bytes
	switch {
	case e.Status >= 500:
		b.S5xx++
	case e.Status >= 400:
		b.S4xx++
	case e.Status >= 300:
		b.S3xx++
	case e.Status >= 200:
		b.S2xx++
	}
	// 401 and 403 only. A 404 is overwhelmingly a broken link or a crawler,
	// and folding it in here would bury the handful of events that actually
	// mean "someone was told no" under thousands that mean "that page moved".
	if e.Status == 401 || e.Status == 403 {
		b.Unauthorized++
	}
	if e.HasDuration {
		b.DurationMsSum += e.DurationMs
		b.DurationCount++
	}

	isError := e.Status >= 500
	w.addDim(unix, minute, DimIP, e.IP, e.Bytes, isError)
	w.addDim(unix, minute, DimPath, e.Path, e.Bytes, isError)
	w.addDim(unix, minute, DimClient, ClientFamily(e.UserAgent), e.Bytes, isError)
	w.addDim(unix, minute, DimNode, source, e.Bytes, isError)
	w.addDim(unix, minute, DimStatus, strconv.Itoa(e.Status), e.Bytes, isError)
	w.addDim(unix, minute, DimMethod, e.Method, e.Bytes, isError)
	// Host and service only exist when the operator put $host and
	// $upstream_addr in their log_format. Recording a placeholder for them
	// would make those panels look populated while saying nothing; leaving
	// the dimension empty is what lets the API tell the UI to explain how to
	// switch them on.
	if e.Host != "" {
		w.addDim(unix, minute, DimHost, e.Host, e.Bytes, isError)
	}
	if e.Service != "" {
		w.addDim(unix, minute, DimService, e.Service, e.Bytes, isError)
	}

	w.addRecent(source, e)
}

func (w *window) addDim(unix int64, minute time.Time, dim, key string, bytes int64, isError bool) {
	if key == "" {
		return
	}
	k := dimKey{minute: unix, dim: dim, key: truncateKey(key)}
	d := w.dims[k]
	if d == nil {
		d = &store.TrafficDim{At: minute, Dim: dim, Key: k.key}
		w.dims[k] = d
	}
	d.Requests++
	d.Bytes += bytes
	if isError {
		d.Errors++
	}
}

// addRecent keeps the newest requestsPerFlush entries of this window. It
// drops from the front rather than refusing new ones once full: the point
// of this list is what happened most recently, so under a burst the newest
// requests are the ones worth keeping.
func (w *window) addRecent(source string, e Entry) {
	r := store.TrafficRequest{
		At: e.At, Source: source, IP: e.IP, Host: e.Host,
		Method: e.Method, Path: e.Path, Status: e.Status, Bytes: e.Bytes,
		UserAgent: truncateKey(e.UserAgent), Service: e.Service,
	}
	if e.HasDuration {
		ms := e.DurationMs
		r.DurationMs = &ms
	}
	w.recent = append(w.recent, r)
	if len(w.recent) > requestsPerFlush {
		w.recent = w.recent[len(w.recent)-requestsPerFlush:]
	}
}

// flush turns the window into the rows to write, applying the per-minute
// top-N cap that keeps the dims table bounded.
func (w *window) flush() store.TrafficFlush {
	out := store.TrafficFlush{Requests: w.recent}
	for _, b := range w.buckets {
		out.Buckets = append(out.Buckets, *b)
	}

	// Group by (minute, dimension) first: a top-N taken across dimensions
	// would let a minute full of distinct IPs crowd out the status codes.
	grouped := map[dimKey][]*store.TrafficDim{}
	for k, d := range w.dims {
		head := dimKey{minute: k.minute, dim: k.dim}
		grouped[head] = append(grouped[head], d)
	}
	for head, entries := range grouped {
		limit := openDimTopN
		switch head.dim {
		case DimStatus, DimMethod, DimNode:
			limit = closedDimTopN
		}
		if len(entries) > limit {
			sort.Slice(entries, func(i, j int) bool { return entries[i].Requests > entries[j].Requests })
			entries = entries[:limit]
		}
		for _, d := range entries {
			out.Dims = append(out.Dims, *d)
		}
	}
	return out
}
