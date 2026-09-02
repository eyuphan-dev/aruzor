package traffic

import (
	"strings"
)

const (
	maxPathLen  = 120
	maxKeyLen   = 160
	idPlacehold = ":id"
)

// NormalizePath makes a URL usable as a grouping key.
//
// Without this the "most requested paths" panel is useless on any real
// site: /orders/8421 and /orders/8422 are the same endpoint under load, and
// a session token in a query string turns every single request into its own
// row. Collapsing variable segments is what turns a list of URLs into a
// list of endpoints, which is the thing an operator actually wants ranked.
func NormalizePath(raw string) string {
	if raw == "" {
		return "-"
	}
	// An absolute-form request line (proxies, and every scanner probing for
	// an open relay) carries the whole URL; only the path part is ours.
	if i := strings.Index(raw, "://"); i >= 0 {
		if j := strings.IndexByte(raw[i+3:], '/'); j >= 0 {
			raw = raw[i+3+j:]
		} else {
			raw = "/"
		}
	}
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	if raw == "" {
		return "/"
	}

	segments := strings.Split(raw, "/")
	for i, seg := range segments {
		if isVariableSegment(seg) {
			segments[i] = idPlacehold
		}
	}
	out := strings.Join(segments, "/")
	if len(out) > maxPathLen {
		out = out[:maxPathLen] + "…"
	}
	return out
}

// isVariableSegment reports whether a path segment is an identifier rather
// than a route name: a number, a UUID, or a long hex/base-ish blob. The
// tests below are deliberately conservative — wrongly collapsing a real
// route name would hide a genuinely popular endpoint, which is a worse
// failure than leaving a few ids uncollapsed.
func isVariableSegment(seg string) bool {
	if seg == "" {
		return false
	}
	if isAllDigits(seg) {
		return true
	}
	if len(seg) == 36 && strings.Count(seg, "-") == 4 && isHexish(seg) {
		return true // UUID
	}
	if len(seg) >= 24 && isHexish(seg) {
		return true // hash, token, object id
	}
	return false
}

func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isHexish(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F', c == '-':
		default:
			return false
		}
	}
	return true
}

// clientRule matches a substring of the User-Agent to a family name. Order
// matters: nearly every browser claims to be Mozilla, Chrome-based ones
// also say Safari, and Edge says both — so the most specific claim has to
// be tested first or everything collapses into "Safari".
var clientRules = []struct {
	needle string
	family string
}{
	// Bots and tools first: many of them impersonate a browser UA, and an
	// operator reading this panel cares far more about "half my traffic is
	// a crawler" than about which browser engine that crawler claims.
	{"googlebot", "Googlebot"},
	{"bingbot", "Bingbot"},
	{"yandexbot", "YandexBot"},
	{"duckduckbot", "DuckDuckBot"},
	{"baiduspider", "Baiduspider"},
	{"ahrefsbot", "AhrefsBot"},
	{"semrushbot", "SemrushBot"},
	{"petalbot", "PetalBot"},
	{"facebookexternalhit", "Facebook"},
	{"twitterbot", "Twitterbot"},
	{"telegrambot", "Telegram"},
	{"whatsapp", "WhatsApp"},
	{"slackbot", "Slack"},
	{"discordbot", "Discord"},
	{"uptimerobot", "UptimeRobot"},
	{"pingdom", "Pingdom"},
	{"censys", "Censys"},
	{"masscan", "masscan"},
	{"zgrab", "zgrab"},
	{"nmap", "Nmap"},
	{"nuclei", "Nuclei"},
	{"sqlmap", "sqlmap"},
	{"curl/", "curl"},
	{"wget", "Wget"},
	{"python-requests", "python-requests"},
	{"python-urllib", "python-urllib"},
	{"go-http-client", "Go HTTP"},
	{"java/", "Java"},
	{"okhttp", "OkHttp"},
	{"axios", "axios"},
	{"postman", "Postman"},
	{"headlesschrome", "Headless Chrome"},
	{"bot", "Diğer bot"},
	{"crawler", "Diğer bot"},
	{"spider", "Diğer bot"},
	{"scanner", "Tarayıcı botu"},
	// Browsers, most specific claim first.
	{"edg/", "Edge"},
	{"edga/", "Edge"},
	{"opr/", "Opera"},
	{"yabrowser", "Yandex Browser"},
	{"samsungbrowser", "Samsung Internet"},
	{"firefox/", "Firefox"},
	{"chrome/", "Chrome"},
	{"safari/", "Safari"},
	{"msie ", "Internet Explorer"},
	{"trident/", "Internet Explorer"},
}

// ClientFamily reduces a raw User-Agent to a name worth ranking.
//
// Storing raw agents as the grouping key would make this panel unreadable
// and unbounded — every Chrome patch release is its own string, and a
// scanner rotating fake agents would push everything real off the list.
// The full agent of any individual request is still visible in the recent
// requests table, so nothing is lost, only summarised.
func ClientFamily(ua string) string {
	if ua == "" {
		return "Bilinmiyor"
	}
	lower := strings.ToLower(ua)
	for _, rule := range clientRules {
		if strings.Contains(lower, rule.needle) {
			return rule.family
		}
	}
	return "Diğer"
}

// truncateKey bounds anything used as a database grouping key. A crafted
// User-Agent or Host header is attacker-controlled input arriving through
// the log file, and it should not be able to write megabyte-long rows.
func truncateKey(s string) string {
	if len(s) > maxKeyLen {
		return s[:maxKeyLen]
	}
	return s
}
