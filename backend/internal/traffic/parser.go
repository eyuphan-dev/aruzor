// Package traffic turns a web server's access log into the traffic
// analytics Aruzor shows on its Traffic page.
//
// Everything on that page — which IP asked the most, which path is hottest,
// which browser or bot is calling, how many 5xx went out, how much
// bandwidth left the box — is only knowable from an access log. Prometheus
// exporters publish counters *about* a server, not the individual requests
// that hit it, so no PromQL can answer "which IP is hammering us right
// now". That is why this package reads log files directly instead of going
// through the Prometheus client like the rest of Aruzor does.
//
// The parser accepts nginx's stock "combined" format with no configuration,
// and picks up three extra fields — virtual host, request duration and
// upstream address — when the operator has added them. Panels that need a
// field the log does not carry say so instead of rendering empty.
package traffic

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// Entry is one parsed request line. Fields the log format did not carry are
// left zero, and the collector reports which ones were missing so the UI can
// explain a blank panel rather than implying there was no traffic.
type Entry struct {
	At        time.Time
	IP        string
	Host      string // $host — empty on the stock combined format
	Method    string
	Path      string // query string stripped, dynamic segments collapsed
	Status    int
	Bytes     int64
	UserAgent string
	Referer   string
	Service   string // $upstream_addr — which backend actually served it

	DurationMs  int64
	HasDuration bool
}

var errUnparsable = errors.New("erisim logu satiri cozumlenemedi")

// nginx's $time_local. The zone offset is part of the line, so timestamps
// stay correct even when Aruzor's own process runs in a different zone.
const timeLayout = "02/Jan/2006:15:04:05 -0700"

// ParseLine reads one access-log line. It is deliberately tolerant: a line
// it cannot make sense of is reported as an error and counted, never
// guessed at — a wrong entry is worse than a missing one on a page whose
// whole job is to say what actually happened.
func ParseLine(line string) (Entry, error) {
	fields := splitFields(line)
	// ip, ident, user, [time], "request", status, bytes — the shortest
	// shape any combined-family format produces.
	if len(fields) < 7 {
		return Entry{}, errUnparsable
	}

	at, err := time.Parse(timeLayout, strings.Trim(fields[3], "[]"))
	if err != nil {
		return Entry{}, errUnparsable
	}

	status, err := strconv.Atoi(fields[5])
	if err != nil || status < 100 || status > 599 {
		return Entry{}, errUnparsable
	}

	e := Entry{
		At:     at,
		IP:     unquote(fields[0]),
		Status: status,
		Bytes:  parseBytes(fields[6]),
	}
	e.Method, e.Path = splitRequest(unquote(fields[4]))
	if len(fields) > 7 {
		e.Referer = blankDash(unquote(fields[7]))
	}
	if len(fields) > 8 {
		e.UserAgent = blankDash(unquote(fields[8]))
	}
	for _, extra := range fields[9:] {
		applyExtra(&e, blankDash(unquote(extra)))
	}
	return e, nil
}

// applyExtra classifies a field that appears past the combined format's
// last column. Matching on shape rather than position means an operator can
// add $host, $request_time and $upstream_addr in any order (or only some of
// them) and still get the panels those fields unlock — asking people to get
// a column order exactly right, with no feedback when they don't, is how
// this kind of feature ends up silently half-working.
func applyExtra(e *Entry, v string) {
	if v == "" {
		return
	}
	// $request_time is the only field that is a plain decimal number:
	// an IPv4 address has three dots and fails to parse, and a hostname
	// fails on its letters.
	if secs, err := strconv.ParseFloat(v, 64); err == nil && secs >= 0 {
		e.DurationMs = int64(secs * 1000)
		e.HasDuration = true
		return
	}
	// $upstream_addr is host:port, a unix: socket path, or a comma-separated
	// list when nginx retried — all of which carry a colon no hostname has.
	if strings.Contains(v, ":") {
		e.Service = v
		return
	}
	e.Host = strings.ToLower(v)
}

// splitFields walks the line splitting on spaces, but keeps a bracketed
// timestamp and a quoted string together — the request line and the user
// agent both contain spaces, so a plain strings.Fields would shred them.
func splitFields(line string) []string {
	var out []string
	var cur strings.Builder
	var closer byte
	inGroup := false

	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case inGroup:
			cur.WriteByte(ch)
			// A backslash-escaped quote is part of the value, not its end;
			// nginx escapes quotes inside $request and $http_user_agent.
			if ch == closer && (i == 0 || line[i-1] != '\\') {
				inGroup = false
				flush()
			}
		case ch == '"' || ch == '[':
			flush()
			inGroup = true
			closer = '"'
			if ch == '[' {
				closer = ']'
			}
			cur.WriteByte(ch)
		case ch == ' ' || ch == '\t':
			flush()
		default:
			cur.WriteByte(ch)
		}
	}
	flush()
	return out
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"') {
		s = s[1 : len(s)-1]
	}
	return strings.ReplaceAll(s, `\"`, `"`)
}

// nginx writes "-" for a field it has no value for. Carrying that through
// would put a literal dash in the "top referrers" list.
func blankDash(s string) string {
	if s == "-" {
		return ""
	}
	return s
}

// $body_bytes_sent is "-" on a request that sent nothing at all.
func parseBytes(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// splitRequest pulls the method and path out of "GET /a/b?c=1 HTTP/1.1".
// Port scanners and TLS probes hitting a plain-HTTP port produce request
// lines that are not requests at all, so anything unrecognisable becomes a
// single "-" bucket rather than thousands of one-off entries in the paths
// panel.
func splitRequest(req string) (method, path string) {
	if req == "" {
		return "-", "-"
	}
	parts := strings.Fields(req)
	if len(parts) < 2 {
		return "-", "-"
	}
	method = strings.ToUpper(parts[0])
	if len(method) > 10 || !isAlpha(method) {
		return "-", "-"
	}
	return method, NormalizePath(parts[1])
}

func isAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return len(s) > 0
}
