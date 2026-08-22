package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// defaultSnoozeMinutes is what a bare /sustur (or the menu button, which
// carries no argument) means. An hour covers a routine deploy or restart
// without risking a silence nobody remembers turning on.
const defaultSnoozeMinutes = 60

// maxSnoozeMinutes caps how long notifications can be silenced from chat.
// A monitoring system that can be muted indefinitely with one message is a
// monitoring system that eventually is — anything longer belongs in the UI,
// where the decision is visible next to the rule.
const maxSnoozeMinutes = 24 * 60

// firingAlertsText lists the rules currently in the firing state. This is
// the question a monitoring bot exists to answer and it was the one thing
// the bot could not be asked: alerts arrived when they changed state, but
// there was no way to ask "what is wrong right now".
func (b *Bot) firingAlertsText(ctx context.Context) string {
	if b.db == nil {
		return "❌ Alarm kuralları okunamıyor."
	}
	rules, err := b.db.ListAlertRules(ctx)
	if err != nil {
		b.log.Warn("alarm kurallari okunamadi", "hata", err.Error())
		return "❌ Alarm kuralları okunamadı."
	}

	var firing, snoozed []string
	enabled := 0
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		enabled++
		if r.LastState != "firing" {
			continue
		}
		if r.SnoozedUntil != nil && r.SnoozedUntil.After(time.Now()) {
			snoozed = append(snoozed, fmt.Sprintf("🔕 %s (%s'e kadar susturulmuş)",
				r.Name, r.SnoozedUntil.Format("15:04")))
			continue
		}
		firing = append(firing, "🔴 "+r.Name)
	}

	if enabled == 0 {
		return "🔔 Alarm Durumu\n\nHiç aktif alarm kuralı yok.\nPanelden kural ekleyebilirsin."
	}
	if len(firing) == 0 && len(snoozed) == 0 {
		return fmt.Sprintf("🟢 Alarm Durumu\n\nHer şey normal.\n%d kural izleniyor.", enabled)
	}

	var sb strings.Builder
	sb.WriteString("🔔 Alarm Durumu\n\n")
	for _, line := range firing {
		sb.WriteString(line + "\n")
	}
	for _, line := range snoozed {
		sb.WriteString(line + "\n")
	}
	fmt.Fprintf(&sb, "\n%d kuraldan %d tanesi alarm veriyor.", enabled, len(firing)+len(snoozed))
	return sb.String()
}

// snoozeText silences every enabled rule for a while. Snoozing all of them
// together rather than one at a time is the deliberate choice: this exists
// for maintenance windows, where the whole system is expected to look
// unhealthy, and picking rules one by one from a chat message would be
// slower than the maintenance itself.
func (b *Bot) snoozeText(ctx context.Context, args string) string {
	if b.db == nil {
		return "❌ Alarm kuralları okunamıyor."
	}

	minutes := defaultSnoozeMinutes
	if trimmed := strings.TrimSpace(args); trimmed != "" {
		parsed, err := strconv.Atoi(strings.Fields(trimmed)[0])
		if err != nil || parsed <= 0 {
			return "Kullanım: /sustur 60\n(kaç dakika susturulacağını yaz)"
		}
		if parsed > maxSnoozeMinutes {
			return fmt.Sprintf("En fazla %d dakika susturulabilir.\nDaha uzunu için panelden kuralı kapat.", maxSnoozeMinutes)
		}
		minutes = parsed
	}

	rules, err := b.db.ListAlertRules(ctx)
	if err != nil {
		b.log.Warn("alarm kurallari okunamadi", "hata", err.Error())
		return "❌ Alarm kuralları okunamadı."
	}

	until := time.Now().Add(time.Duration(minutes) * time.Minute)
	count := 0
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if err := b.db.SetAlertRuleSnooze(ctx, r.ID, &until); err != nil {
			b.log.Warn("alarm susturulamadi", "kural", r.Name, "hata", err.Error())
			continue
		}
		count++
	}

	if count == 0 {
		return "Susturulacak aktif kural yok."
	}
	b.log.Info("telegram uzerinden alarmlar susturuldu", "kural_sayisi", count, "bitis", until)
	return fmt.Sprintf("🔕 %d kural %s'e kadar susturuldu.\n\nO saate kadar bildirim gitmeyecek; alarmlar izlenmeye devam ediyor.",
		count, until.Format("15:04"))
}

// monitorUptimeWindow matches the window the UI and the public status page
// report over, so the same monitor does not show two different percentages
// depending on where it is read.
const monitorUptimeWindow = 30 * 24 * time.Hour

// monitorsText answers /izleme. The public reply carries a monitor's name,
// whether it is up, and its uptime percentage — never its target address.
// That address is the map of what runs where on the internal network, and
// this bot posts to a chat whose membership the operator does not fully
// control. The development bot, which is private by construction, prints
// the full detail instead.
func (b *Bot) monitorsText(ctx context.Context) string {
	if b.devEnabled() {
		return b.monitorsDetailText(ctx)
	}
	if b.db == nil {
		return "❌ Veritabanı bağlı değil."
	}
	monitors, err := b.db.ListMonitors(ctx)
	if err != nil {
		return "❌ İzlemeler okunamadı: " + err.Error()
	}
	if len(monitors) == 0 {
		return "📡 İzlemeler\n\nHenüz izleme eklenmemiş."
	}

	var sb strings.Builder
	sb.WriteString("📡 İzlemeler\n\n")
	for _, m := range monitors {
		icon := "⚪"
		switch {
		case m.LastOK == nil:
			icon = "⚪" // never checked yet
		case *m.LastOK:
			icon = "🟢"
		default:
			icon = "🔴"
		}
		fmt.Fprintf(&sb, "%s %s", icon, m.Name)
		if pct, ok, err := b.db.UptimePercent(ctx, m.ID, time.Now().Add(-monitorUptimeWindow)); err == nil && ok {
			fmt.Fprintf(&sb, " — %%%.2f", pct)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n30 günlük çalışma oranı.")
	return sb.String()
}

// monitorDigestLine is the digest's monitor section: one line per monitor,
// same sanitized fields as the public reply. Returns "" when there is
// nothing to report, so a deployment with no monitors gets no empty heading.
func (b *Bot) monitorDigestLine(ctx context.Context) string {
	if b.db == nil {
		return ""
	}
	monitors, err := b.db.ListMonitors(ctx)
	if err != nil {
		b.log.Warn("ozet icin izlemeler okunamadi", "hata", err.Error())
		return ""
	}
	if len(monitors) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("📡 İzlemeler")
	for _, m := range monitors {
		icon := "⚪"
		switch {
		case m.LastOK == nil:
		case *m.LastOK:
			icon = "🟢"
		default:
			icon = "🔴"
		}
		sb.WriteString("\n")
		fmt.Fprintf(&sb, "%s %s", icon, m.Name)
		if pct, ok, err := b.db.UptimePercent(ctx, m.ID, time.Now().Add(-monitorUptimeWindow)); err == nil && ok {
			fmt.Fprintf(&sb, " — %%%.2f", pct)
		}
	}
	return sb.String()
}
