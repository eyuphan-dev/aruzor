package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Development mode turns the bot from a deliberately minimal status
// reporter into a full diagnostic console. The production bot is
// aggregate-only on purpose — it posts to a shared chat, so it never
// reveals hostnames, mount points, target addresses or query text. A
// development bot has a different threat model: a private chat with a
// single operator, used to debug the very things production hides.
//
// The two are separated by *bot token*, not by a flag on one bot, so
// there is no way for a misconfigured switch to start leaking detail into
// the production group. A development bot needs its own token and its own
// chat, and dev commands are refused outright unless the mode is on.
const (
	devCommandLimit = 12 // series/rows per reply, keeps messages under Telegram's 4096-char cap
	auditLogLimit   = 10
)

var devCommands = []BotCommand{
	{Command: "detay", Description: "Ayrıntılı sistem durumu (yük, takas, bölüm bölüm disk)"},
	{Command: "hedefler", Description: "Prometheus hedeflerinin durumu ve son hataları"},
	{Command: "alarmlar", Description: "Kural detayları: PromQL, eşik, susturma (/alarm sadece durumu verir)"},
	{Command: "loglar", Description: "Son denetim kayıtları"},
	{Command: "surum", Description: "Süreç bilgisi: çalışma süresi, bellek, goroutine"},
	{Command: "sorgu", Description: "Ham PromQL çalıştır: /sorgu up"},
}

// startedAt is process start, used by /surum. Package-level so it reflects
// the process rather than when the bot happened to be constructed.
var startedAt = time.Now()

func (b *Bot) devEnabled() bool { return b.devMode }

// handleDevCommand answers the development-only commands. It returns
// ("", false) when the command isn't one of them, so the caller can fall
// through to the shared command set.
func (b *Bot) handleDevCommand(ctx context.Context, command, args string) (string, bool) {
	if !b.devEnabled() {
		return "", false
	}
	switch command {
	case "/detay":
		return b.detailText(ctx), true
	case "/hedefler":
		return b.targetsText(ctx), true
	case "/alarmlar":
		return b.alertRulesText(ctx), true
	case "/loglar":
		return b.auditText(ctx), true
	case "/surum":
		return b.runtimeText(ctx), true
	case "/sorgu":
		return b.queryText(ctx, args), true
	}
	return "", false
}

// detailQueries are the extra readings a developer usually wants next
// after the three headline percentages: what the load actually is, whether
// swap is being touched, and which mount point is filling up.
var detailQueries = []struct {
	label  string
	promQL string
	unit   string
}{
	{"Yük (1dk)", `sum(node_load1)`, ""},
	{"Yük (5dk)", `sum(node_load5)`, ""},
	{"Yük (15dk)", `sum(node_load15)`, ""},
	{"Çekirdek", `count(count(node_cpu_seconds_total) by (cpu))`, ""},
	{"Bellek toplam", `sum(node_memory_MemTotal_bytes)`, "bytes"},
	{"Bellek boşta", `sum(node_memory_MemAvailable_bytes)`, "bytes"},
	{"Takas kullanımı", `sum(node_memory_SwapTotal_bytes - node_memory_SwapFree_bytes)`, "bytes"},
	{"Ağ giriş", `sum(rate(node_network_receive_bytes_total{device!="lo"}[5m]))`, "bps"},
	{"Ağ çıkış", `sum(rate(node_network_transmit_bytes_total{device!="lo"}[5m]))`, "bps"},
	{"Çalışma süresi", `time() - node_boot_time_seconds`, "duration"},
}

func (b *Bot) detailText(ctx context.Context) string {
	var sb strings.Builder
	sb.WriteString("🔧 Ayrıntılı Sistem Durumu\n\n")

	for _, q := range detailQueries {
		v, ok := b.scalar(ctx, q.promQL)
		if !ok {
			fmt.Fprintf(&sb, "⚪ %s: veri yok\n", q.label)
			continue
		}
		fmt.Fprintf(&sb, "• %s: %s\n", q.label, formatUnit(v, q.unit))
	}

	sb.WriteString("\n💽 Disk (bölüm bölüm)\n")
	rows := b.vector(ctx, `(1 - (node_filesystem_avail_bytes{fstype!="tmpfs"} / node_filesystem_size_bytes{fstype!="tmpfs"})) * 100`)
	if len(rows) == 0 {
		sb.WriteString("veri yok\n")
	}
	for _, r := range rows {
		mount := r.labels["mountpoint"]
		if mount == "" {
			mount = r.labels["device"]
		}
		fmt.Fprintf(&sb, "• %s: %%%.1f\n", mount, r.value)
	}

	fmt.Fprintf(&sb, "\n🕒 %s", time.Now().Format("02.01.2006 15:04:05"))
	return sb.String()
}

func (b *Bot) targetsText(ctx context.Context) string {
	data, err := b.prom.Targets(ctx)
	if err != nil {
		return "❌ Hedefler alınamadı: " + err.Error()
	}
	var parsed struct {
		ActiveTargets []struct {
			Labels     map[string]string `json:"labels"`
			ScrapePool string            `json:"scrapePool"`
			Health     string            `json:"health"`
			LastError  string            `json:"lastError"`
			LastScrape time.Time         `json:"lastScrape"`
		} `json:"activeTargets"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "❌ Hedef yanıtı çözümlenemedi: " + err.Error()
	}
	if len(parsed.ActiveTargets) == 0 {
		return "🎯 Prometheus Hedefleri\n\nHiç aktif hedef yok."
	}

	var sb strings.Builder
	sb.WriteString("🎯 Prometheus Hedefleri\n\n")
	for _, t := range parsed.ActiveTargets {
		icon := "🟢"
		if t.Health != "up" {
			icon = "🔴"
		}
		fmt.Fprintf(&sb, "%s %s — %s\n", icon, t.ScrapePool, t.Labels["instance"])
		fmt.Fprintf(&sb, "   son tarama: %s\n", relative(t.LastScrape))
		if t.LastError != "" {
			fmt.Fprintf(&sb, "   ⚠️ %s\n", t.LastError)
		}
	}
	return sb.String()
}

func (b *Bot) alertRulesText(ctx context.Context) string {
	if b.db == nil {
		return "❌ Veritabanı bağlı değil."
	}
	rules, err := b.db.ListAlertRules(ctx)
	if err != nil {
		return "❌ Alarm kuralları okunamadı: " + err.Error()
	}
	if len(rules) == 0 {
		return "🔔 Alarm Kuralları\n\nHenüz kural yok."
	}

	var sb strings.Builder
	sb.WriteString("🔔 Alarm Kuralları\n\n")
	for _, r := range rules {
		icon := "🟢"
		switch {
		case !r.Enabled:
			icon = "⚫"
		case r.LastState == "firing":
			icon = "🔴"
		}
		fmt.Fprintf(&sb, "%s %s  (%s %g)\n", icon, r.Name, r.Operator, r.Threshold)
		fmt.Fprintf(&sb, "   %s\n", r.PromQL)
		if r.SnoozedUntil != nil && r.SnoozedUntil.After(time.Now()) {
			fmt.Fprintf(&sb, "   🔕 %s'e kadar susturulmuş\n", r.SnoozedUntil.Format("02.01 15:04"))
		}
		if r.LastNotifiedAt != nil {
			fmt.Fprintf(&sb, "   son bildirim: %s\n", relative(*r.LastNotifiedAt))
		}
	}
	return sb.String()
}

// monitorsDetailText is the development-bot view: it names the target
// address, which the public reply must never do.
func (b *Bot) monitorsDetailText(ctx context.Context) string {
	if b.db == nil {
		return "❌ Veritabanı bağlı değil."
	}
	monitors, err := b.db.ListMonitors(ctx)
	if err != nil {
		return "❌ İzlemeler okunamadı: " + err.Error()
	}
	if len(monitors) == 0 {
		return "📡 İzlemeler\n\nHenüz izleme yok."
	}

	var sb strings.Builder
	sb.WriteString("📡 İzlemeler\n\n")
	for _, m := range monitors {
		icon := "⚪"
		if m.LastOK != nil {
			icon = "🔴"
			if *m.LastOK {
				icon = "🟢"
			}
		}
		fmt.Fprintf(&sb, "%s %s (%s)\n   %s\n", icon, m.Name, m.Type, m.Target)
		if m.LastLatencyMs != nil {
			fmt.Fprintf(&sb, "   gecikme: %dms", *m.LastLatencyMs)
			if m.LastCheckedAt != nil {
				fmt.Fprintf(&sb, " · %s", relative(*m.LastCheckedAt))
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func (b *Bot) auditText(ctx context.Context) string {
	if b.db == nil {
		return "❌ Veritabanı bağlı değil."
	}
	logs, err := b.db.ListAuditLogs(ctx, auditLogLimit)
	if err != nil {
		return "❌ Denetim kayıtları okunamadı: " + err.Error()
	}
	if len(logs) == 0 {
		return "📜 Son Denetim Kayıtları\n\nKayıt yok."
	}

	var sb strings.Builder
	sb.WriteString("📜 Son Denetim Kayıtları\n\n")
	for _, l := range logs {
		fmt.Fprintf(&sb, "• %s  %s\n   %s · %s\n",
			l.CreatedAt.Format("02.01 15:04"), l.Event, l.Email, l.RemoteAddr)
	}
	return sb.String()
}

func (b *Bot) runtimeText(ctx context.Context) string {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	var sb strings.Builder
	sb.WriteString("⚙️ Süreç Bilgisi\n\n")
	fmt.Fprintf(&sb, "• Go: %s (%s/%s)\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&sb, "• Çalışma süresi: %s\n", shortDuration(time.Since(startedAt)))
	fmt.Fprintf(&sb, "• Goroutine: %d\n", runtime.NumGoroutine())
	fmt.Fprintf(&sb, "• Heap: %s\n", formatBytes(float64(mem.HeapAlloc)))
	fmt.Fprintf(&sb, "• Toplam ayrılan: %s\n", formatBytes(float64(mem.TotalAlloc)))
	fmt.Fprintf(&sb, "• GC sayısı: %d\n", mem.NumGC)
	fmt.Fprintf(&sb, "• Zaman dilimi: %s (%s)\n", time.Local.String(), time.Now().Format("-07:00"))

	if path := os.Getenv("ARUZOR_DB_PATH"); path != "" {
		if info, err := os.Stat(path); err == nil {
			fmt.Fprintf(&sb, "• Veritabanı: %s\n", formatBytes(float64(info.Size())))
		}
	}
	if b.db != nil {
		if last, ok, err := b.db.GetSetting(ctx, "alerts.last_digest_date"); err == nil && ok {
			fmt.Fprintf(&sb, "• Son günlük özet: %s\n", last)
		}
	}
	return sb.String()
}

// queryText runs an arbitrary instant PromQL query. This is the reason a
// separate development bot exists at all — it is far too much power for
// the production chat, where an accidental query could dump every label on
// the system into a group conversation.
func (b *Bot) queryText(ctx context.Context, args string) string {
	promQL := strings.TrimSpace(args)
	if promQL == "" {
		return "Kullanım: /sorgu <promql>\nÖrnek: /sorgu up"
	}

	rows := b.vector(ctx, promQL)
	if rows == nil {
		return "❌ Sorgu çalıştırılamadı. PromQL geçerli mi?"
	}
	if len(rows) == 0 {
		return "Sonuç boş."
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "🔎 %s\n\n", promQL)
	for i, r := range rows {
		if i == devCommandLimit {
			fmt.Fprintf(&sb, "\n… %d seri daha var, sorguyu daraltın.", len(rows)-devCommandLimit)
			break
		}
		fmt.Fprintf(&sb, "• %s = %g\n", labelSetString(r.labels), r.value)
	}
	return sb.String()
}

type sample struct {
	labels map[string]string
	value  float64
}

// vector runs an instant query and returns its samples. A nil return means
// the query itself failed; an empty non-nil slice means it ran and matched
// nothing — the caller reports those differently.
func (b *Bot) vector(ctx context.Context, promQL string) []sample {
	data, err := b.prom.Query(ctx, promQL, time.Now())
	if err != nil {
		return nil
	}
	var parsed struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  []json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}

	out := make([]sample, 0, len(parsed.Result))
	for _, r := range parsed.Result {
		if len(r.Value) < 2 {
			continue
		}
		var raw string
		if err := json.Unmarshal(r.Value[1], &raw); err != nil {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			continue
		}
		out = append(out, sample{labels: r.Metric, value: v})
	}
	return out
}

func (b *Bot) scalar(ctx context.Context, promQL string) (float64, bool) {
	rows := b.vector(ctx, promQL)
	if len(rows) == 0 {
		return 0, false
	}
	return rows[0].value, true
}

func labelSetString(labels map[string]string) string {
	if len(labels) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		if k != "__name__" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	name := labels["__name__"]
	if len(parts) == 0 {
		if name == "" {
			return "{}"
		}
		return name
	}
	return name + "{" + strings.Join(parts, ", ") + "}"
}

func formatUnit(v float64, unit string) string {
	switch unit {
	case "bytes":
		return formatBytes(v)
	case "bps":
		return formatBytes(v) + "/sn"
	case "duration":
		return shortDuration(time.Duration(v) * time.Second)
	default:
		return fmt.Sprintf("%.2f", v)
	}
}

func formatBytes(v float64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%.0f B", v)
	}
	div, exp := float64(unit), 0
	for n := v / unit; n >= unit && exp < 3; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", v/div, "KMGT"[exp])
}

func shortDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0f sn", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0f dk", d.Minutes())
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1f sa", d.Hours())
	}
	return fmt.Sprintf("%.1f gün", d.Hours()/24)
}

func relative(t time.Time) string {
	if t.IsZero() {
		return "hiç"
	}
	return shortDuration(time.Since(t)) + " önce"
}
