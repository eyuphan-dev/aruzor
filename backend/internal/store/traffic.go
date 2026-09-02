package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Traffic analytics storage.
//
// Access logs are the highest-volume thing Aruzor touches, and the whole
// point of the Traffic page is that it stays usable on the same small
// SQLite file as everything else. So raw lines are never stored. What is
// stored is three shapes, each bounded by construction:
//
//   - traffic_buckets: one row per minute, totals only. A fixed 1440 rows a
//     day whether the site served ten requests or ten million.
//   - traffic_dims: per-minute top-N per dimension. Bounded by the top-N cap
//     the collector applies before writing, not by how many distinct IPs,
//     paths or user agents actually appeared.
//   - traffic_requests: a short tail of individual requests for the recent
//     requests table, pruned to a fixed row count on every flush.
//
// The cost of that bounding is that a rank summed across many minutes is
// slightly approximate: a key that never made any single minute's top-N
// contributes nothing to the total. For ranking "who is hitting us hardest"
// that is the right trade — the entries it can lose are by definition the
// ones nobody is looking for.

// TrafficSource is one access-log file being followed, plus where reading
// last stopped so a restart resumes instead of re-counting the whole file.
type TrafficSource struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Path       string     `json:"path"`
	ReadOffset int64      `json:"-"`
	Lines      int64      `json:"lines"`
	Unparsed   int64      `json:"unparsed"`
	LastReadAt *time.Time `json:"lastReadAt,omitempty"`
}

// TrafficBucket is one minute of totals across every source.
type TrafficBucket struct {
	At            time.Time `json:"at"`
	Requests      int64     `json:"requests"`
	Bytes         int64     `json:"bytes"`
	S2xx          int64     `json:"s2xx"`
	S3xx          int64     `json:"s3xx"`
	S4xx          int64     `json:"s4xx"`
	S5xx          int64     `json:"s5xx"`
	Unauthorized  int64     `json:"unauthorized"`
	DurationMsSum int64     `json:"-"`
	DurationCount int64     `json:"-"`
}

// TrafficDim is one ranked entry: an IP, a path, a client family, a host,
// an upstream service, a node, a status code or a method.
type TrafficDim struct {
	// At is the minute this entry was counted in, taken from the log line's
	// own timestamp rather than from when Aruzor read it. Backfilling an
	// existing log file would otherwise pile hours of history into the
	// current minute and make "top IPs in the last hour" include yesterday.
	At       time.Time `json:"-"`
	Dim      string    `json:"-"`
	Key      string    `json:"key"`
	Requests int64     `json:"requests"`
	Bytes    int64     `json:"bytes"`
	Errors   int64     `json:"errors"`
}

// TrafficRequest is a single logged request, kept only for the recent
// requests table.
type TrafficRequest struct {
	ID         int64     `json:"id"`
	At         time.Time `json:"at"`
	Source     string    `json:"source"`
	IP         string    `json:"ip"`
	Host       string    `json:"host,omitempty"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Status     int       `json:"status"`
	Bytes      int64     `json:"bytes"`
	UserAgent  string    `json:"userAgent,omitempty"`
	Service    string    `json:"service,omitempty"`
	DurationMs *int64    `json:"durationMs,omitempty"`
}

// TrafficFlush is everything one collector flush writes, applied as a
// single transaction so a reader never sees a half-written minute.
type TrafficFlush struct {
	Buckets  []TrafficBucket
	Dims     []TrafficDim
	Requests []TrafficRequest
	Sources  []TrafficSource
}

func (s *Store) migrateTraffic() error {
	schema := `
	CREATE TABLE IF NOT EXISTS traffic_sources (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		path TEXT NOT NULL,
		read_offset INTEGER NOT NULL DEFAULT 0,
		lines INTEGER NOT NULL DEFAULT 0,
		unparsed INTEGER NOT NULL DEFAULT 0,
		last_read_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS traffic_buckets (
		bucket_at DATETIME PRIMARY KEY,
		requests INTEGER NOT NULL DEFAULT 0,
		bytes INTEGER NOT NULL DEFAULT 0,
		s2xx INTEGER NOT NULL DEFAULT 0,
		s3xx INTEGER NOT NULL DEFAULT 0,
		s4xx INTEGER NOT NULL DEFAULT 0,
		s5xx INTEGER NOT NULL DEFAULT 0,
		unauthorized INTEGER NOT NULL DEFAULT 0,
		duration_ms_sum INTEGER NOT NULL DEFAULT 0,
		duration_count INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS traffic_dims (
		bucket_at DATETIME NOT NULL,
		dim TEXT NOT NULL,
		key TEXT NOT NULL,
		requests INTEGER NOT NULL DEFAULT 0,
		bytes INTEGER NOT NULL DEFAULT 0,
		errors INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (bucket_at, dim, key)
	);

	CREATE TABLE IF NOT EXISTS traffic_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		at DATETIME NOT NULL,
		source TEXT NOT NULL DEFAULT '',
		ip TEXT NOT NULL DEFAULT '',
		host TEXT NOT NULL DEFAULT '',
		method TEXT NOT NULL DEFAULT '',
		path TEXT NOT NULL DEFAULT '',
		status INTEGER NOT NULL DEFAULT 0,
		bytes INTEGER NOT NULL DEFAULT 0,
		user_agent TEXT NOT NULL DEFAULT '',
		service TEXT NOT NULL DEFAULT '',
		duration_ms INTEGER
	);

	CREATE INDEX IF NOT EXISTS idx_traffic_dims_at ON traffic_dims(bucket_at);
	CREATE INDEX IF NOT EXISTS idx_traffic_requests_at ON traffic_requests(at);
	`
	_, err := s.db.Exec(schema)
	return err
}

// SaveTrafficFlush writes one collector flush. Counters are added to
// whatever is already there rather than replacing it: two sources can flush
// into the same minute, and a restart mid-minute must not discard what the
// previous process already recorded for it.
func (s *Store) SaveTrafficFlush(ctx context.Context, f TrafficFlush) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // a no-op once Commit has succeeded

	for _, b := range f.Buckets {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO traffic_buckets
				(bucket_at, requests, bytes, s2xx, s3xx, s4xx, s5xx, unauthorized, duration_ms_sum, duration_count)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(bucket_at) DO UPDATE SET
				requests = requests + excluded.requests,
				bytes = bytes + excluded.bytes,
				s2xx = s2xx + excluded.s2xx,
				s3xx = s3xx + excluded.s3xx,
				s4xx = s4xx + excluded.s4xx,
				s5xx = s5xx + excluded.s5xx,
				unauthorized = unauthorized + excluded.unauthorized,
				duration_ms_sum = duration_ms_sum + excluded.duration_ms_sum,
				duration_count = duration_count + excluded.duration_count`,
			b.At.UTC(), b.Requests, b.Bytes, b.S2xx, b.S3xx, b.S4xx, b.S5xx, b.Unauthorized, b.DurationMsSum, b.DurationCount,
		); err != nil {
			return err
		}
	}

	for _, d := range f.Dims {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO traffic_dims (bucket_at, dim, key, requests, bytes, errors)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(bucket_at, dim, key) DO UPDATE SET
				requests = requests + excluded.requests,
				bytes = bytes + excluded.bytes,
				errors = errors + excluded.errors`,
			d.At.UTC(), d.Dim, d.Key, d.Requests, d.Bytes, d.Errors,
		); err != nil {
			return err
		}
	}

	for _, r := range f.Requests {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO traffic_requests (at, source, ip, host, method, path, status, bytes, user_agent, service, duration_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.At.UTC(), r.Source, r.IP, r.Host, r.Method, r.Path, r.Status, r.Bytes, r.UserAgent, r.Service, r.DurationMs,
		); err != nil {
			return err
		}
	}

	for _, src := range f.Sources {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO traffic_sources (id, name, path, read_offset, lines, unparsed, last_read_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name = excluded.name,
				path = excluded.path,
				read_offset = excluded.read_offset,
				lines = excluded.lines,
				unparsed = excluded.unparsed,
				last_read_at = excluded.last_read_at`,
			src.ID, src.Name, src.Path, src.ReadOffset, src.Lines, src.Unparsed, src.LastReadAt,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) ListTrafficSources(ctx context.Context) ([]TrafficSource, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, path, read_offset, lines, unparsed, last_read_at FROM traffic_sources ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TrafficSource{}
	for rows.Next() {
		var src TrafficSource
		var last sql.NullTime
		if err := rows.Scan(&src.ID, &src.Name, &src.Path, &src.ReadOffset, &src.Lines, &src.Unparsed, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			t := last.Time
			src.LastReadAt = &t
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// TrafficSeries returns the per-minute totals from since onwards, oldest
// first. Minutes in which nothing was served have no row at all; filling
// those gaps is left to the caller, because "served nothing" and "was not
// running" are different facts and only the caller knows which it means.
func (s *Store) TrafficSeries(ctx context.Context, since time.Time) ([]TrafficBucket, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT bucket_at, requests, bytes, s2xx, s3xx, s4xx, s5xx, unauthorized, duration_ms_sum, duration_count
		FROM traffic_buckets WHERE bucket_at >= ? ORDER BY bucket_at`, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TrafficBucket{}
	for rows.Next() {
		var b TrafficBucket
		if err := rows.Scan(&b.At, &b.Requests, &b.Bytes, &b.S2xx, &b.S3xx, &b.S4xx, &b.S5xx,
			&b.Unauthorized, &b.DurationMsSum, &b.DurationCount); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// TrafficRanking returns the top entries of one dimension since a given
// time. orderByBytes ranks by bandwidth instead of request count: the same
// dimension answers two different questions — who calls most often, and who
// costs the most to serve — and they routinely have different answers.
func (s *Store) TrafficRanking(ctx context.Context, dim string, since time.Time, orderByBytes bool, limit int) ([]TrafficDim, error) {
	order := "total_requests DESC"
	if orderByBytes {
		order = "total_bytes DESC"
	}
	// dim and since arrive as bind parameters; only the ORDER BY clause is
	// interpolated, and it is one of two constants chosen here.
	query := fmt.Sprintf(`
		SELECT key,
		       SUM(requests) AS total_requests,
		       SUM(bytes)    AS total_bytes,
		       SUM(errors)   AS total_errors
		FROM traffic_dims
		WHERE bucket_at >= ? AND dim = ?
		GROUP BY key
		ORDER BY %s
		LIMIT ?`, order)

	rows, err := s.db.QueryContext(ctx, query, since.UTC(), dim, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TrafficDim{}
	for rows.Next() {
		var d TrafficDim
		if err := rows.Scan(&d.Key, &d.Requests, &d.Bytes, &d.Errors); err != nil {
			return nil, err
		}
		d.Dim = dim
		out = append(out, d)
	}
	return out, rows.Err()
}

// TrafficErrorPaths ranks the paths that actually returned 5xx. That is a
// different list from the busiest paths — the endpoint that is broken is
// usually not the one that is popular — so it gets its own query rather
// than being read off the top-paths panel.
func (s *Store) TrafficErrorPaths(ctx context.Context, since time.Time, limit int) ([]TrafficDim, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, SUM(requests), SUM(bytes), SUM(errors)
		FROM traffic_dims
		WHERE bucket_at >= ? AND dim = 'path' AND errors > 0
		GROUP BY key
		ORDER BY SUM(errors) DESC
		LIMIT ?`, since.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TrafficDim{}
	for rows.Next() {
		var d TrafficDim
		if err := rows.Scan(&d.Key, &d.Requests, &d.Bytes, &d.Errors); err != nil {
			return nil, err
		}
		d.Dim = "path"
		out = append(out, d)
	}
	return out, rows.Err()
}

// TrafficRequestFilter selects which slice of the recent-request tail to
// return. The unauthorized and error filters exist as their own queries
// because scrolling a busy site's full request list looking for the 403s is
// not a realistic way to notice someone probing the admin panel.
type TrafficRequestFilter string

const (
	TrafficAll          TrafficRequestFilter = "all"
	TrafficUnauthorized TrafficRequestFilter = "unauthorized"
	TrafficErrors       TrafficRequestFilter = "errors"
)

// since scopes the result to the range the page is showing. Without it a
// panel sitting under a "last 24 hours" selector would list a request from
// last week, because this table is pruned by row count rather than by age.
func (s *Store) ListTrafficRequests(ctx context.Context, filter TrafficRequestFilter, since time.Time, limit int) ([]TrafficRequest, error) {
	where := "WHERE at >= ?"
	switch filter {
	case TrafficUnauthorized:
		where += " AND status IN (401, 403)"
	case TrafficErrors:
		where += " AND status >= 500"
	}
	query := fmt.Sprintf(`
		SELECT id, at, source, ip, host, method, path, status, bytes, user_agent, service, duration_ms
		FROM traffic_requests %s ORDER BY at DESC, id DESC LIMIT ?`, where)

	rows, err := s.db.QueryContext(ctx, query, since.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TrafficRequest{}
	for rows.Next() {
		var r TrafficRequest
		var dur sql.NullInt64
		if err := rows.Scan(&r.ID, &r.At, &r.Source, &r.IP, &r.Host, &r.Method, &r.Path,
			&r.Status, &r.Bytes, &r.UserAgent, &r.Service, &dur); err != nil {
			return nil, err
		}
		if dur.Valid {
			v := dur.Int64
			r.DurationMs = &v
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PruneTraffic enforces both bounds: aggregates age out by time, individual
// requests by row count. Without the second one a busy site would grow the
// database without limit even though nothing older than the last screenful
// of requests is ever displayed.
func (s *Store) PruneTraffic(ctx context.Context, before time.Time, keepRequests int) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM traffic_buckets WHERE bucket_at < ?`, before.UTC()); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM traffic_dims WHERE bucket_at < ?`, before.UTC()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM traffic_requests
		WHERE id NOT IN (SELECT id FROM traffic_requests ORDER BY id DESC LIMIT ?)`, keepRequests)
	return err
}

// HasTrafficData reports whether anything has ever been ingested, so a
// fresh install can show setup instructions instead of a page of zeroes
// that reads as a broken dashboard.
func (s *Store) HasTrafficData(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM traffic_buckets`).Scan(&n)
	return n > 0, err
}
