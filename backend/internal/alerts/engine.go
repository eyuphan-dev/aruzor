// Package alerts periodically evaluates user-defined threshold rules
// against Prometheus and notifies Telegram only when a rule's state
// changes (ok -> firing or firing -> ok), plus a fixed daily health digest.
package alerts

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"

	"aruzor/internal/prometheus"
	"aruzor/internal/store"
)

type Notifier interface {
	SendAlert(ctx context.Context, text string) error
	SendDailyDigest(ctx context.Context) error
}

type Engine struct {
	db           *store.Store
	prom         *prometheus.Client
	notifier     Notifier // nil when Telegram isn't configured
	log          *slog.Logger
	evalInterval time.Duration
	digestHour   int
}

func NewEngine(db *store.Store, prom *prometheus.Client, notifier Notifier, log *slog.Logger, evalInterval time.Duration, digestHour int) *Engine {
	return &Engine{db: db, prom: prom, notifier: notifier, log: log, evalInterval: evalInterval, digestHour: digestHour}
}

// Run blocks until ctx is canceled, periodically evaluating rules and
// sending the daily digest once per day at digestHour.
func (e *Engine) Run(ctx context.Context) {
	evalTicker := time.NewTicker(e.evalInterval)
	defer evalTicker.Stop()

	digestTicker := time.NewTicker(time.Minute)
	defer digestTicker.Stop()

	e.maybeSendDigest(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-evalTicker.C:
			e.evaluateRules(ctx)
		case <-digestTicker.C:
			e.maybeSendDigest(ctx)
		}
	}
}

// lastDigestKey stores the date (YYYY-MM-DD) of the most recent digest.
// It lives in the database rather than in memory because the process is
// restarted on every deploy: an in-memory marker meant a restart during
// the digest hour silently swallowed that day's digest, and a restart
// after it could send a second one.
const lastDigestKey = "alerts.last_digest_date"

// maybeSendDigest sends today's digest if the digest hour has arrived and
// today's hasn't gone out yet. It deliberately checks ">= digestHour"
// rather than "== digestHour" so a process that was down for that whole
// hour still catches up when it comes back, instead of skipping the day
// with nothing to show for it.
func (e *Engine) maybeSendDigest(ctx context.Context) {
	if e.notifier == nil {
		return
	}
	now := time.Now()
	if now.Hour() < e.digestHour {
		return
	}
	today := now.Format("2006-01-02")

	last, _, err := e.db.GetSetting(ctx, lastDigestKey)
	if err != nil {
		e.log.Warn("son ozet tarihi okunamadi", "hata", err.Error())
		return
	}
	if last == today {
		return
	}

	// Recorded before sending: a Telegram failure that keeps failing must
	// not turn into a message every minute for the rest of the day.
	if err := e.db.SetSetting(ctx, lastDigestKey, today); err != nil {
		e.log.Warn("ozet tarihi kaydedilemedi", "hata", err.Error())
		return
	}
	e.sendDigest(ctx)
}

func (e *Engine) sendDigest(ctx context.Context) {
	if e.notifier == nil {
		return
	}
	if err := e.notifier.SendDailyDigest(ctx); err != nil {
		e.log.Warn("gunluk ozet gonderilemedi", "hata", err.Error())
		return
	}
	// Logged on success too: without it there is no way to tell "the digest
	// went out" apart from "the schedule never fired" when someone reports a
	// missing message.
	e.log.Info("gunluk ozet gonderildi", "saat", time.Now().Format("2006-01-02 15:04:05 MST"))
}

func (e *Engine) evaluateRules(ctx context.Context) {
	rules, err := e.db.ListAlertRules(ctx)
	if err != nil {
		e.log.Error("alarm kurallari okunamadi", "hata", err.Error())
		return
	}

	// Collect every rule's transition message from this tick instead of
	// sending immediately: if a dependency (e.g. a whole server) went down
	// and several rules fire in the same tick, that's one combined Telegram
	// message instead of N separate ones spamming the chat.
	var messages []string
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if msg := e.evaluateRule(ctx, rule); msg != "" {
			messages = append(messages, msg)
		}
	}
	e.sendBatch(ctx, messages)
}

// evaluateRule returns the notification text for this rule's transition (if
// any occurred and it isn't snoozed), or "" if nothing should be sent.
func (e *Engine) evaluateRule(ctx context.Context, rule store.AlertRule) string {
	value, ok := e.queryValue(ctx, rule.PromQL)
	if !ok {
		return ""
	}

	firing := compare(value, rule.Operator, rule.Threshold)
	newState := "ok"
	if firing {
		newState = "firing"
	}

	if newState == rule.LastState {
		return ""
	}

	if err := e.db.UpdateAlertRuleState(ctx, rule.ID, newState, time.Now()); err != nil {
		e.log.Error("alarm durumu guncellenemedi", "hata", err.Error())
		return ""
	}

	event := "resolved"
	if newState == "firing" {
		event = "fired"
	}
	if err := e.db.InsertAlertEvent(ctx, uuid.NewString(), rule.ID, rule.Name, event, value); err != nil {
		e.log.Error("alarm gecmisi kaydedilemedi", "hata", err.Error())
	}

	e.log.Info("alarm durumu degisti", "kural", rule.Name, "eski", rule.LastState, "yeni", newState, "deger", value)

	if rule.SnoozedUntil != nil && rule.SnoozedUntil.After(time.Now()) {
		e.log.Info("alarm susturulmus, bildirim atlandi", "kural", rule.Name, "susturma_bitis", rule.SnoozedUntil)
		return ""
	}

	return formatAlertMessage(rule, newState, value)
}

// sendBatch sends a single message for one transition (unchanged format
// from before batching existed), or one combined message when several
// rules changed state in the same evaluation tick.
func (e *Engine) sendBatch(ctx context.Context, messages []string) {
	if e.notifier == nil || len(messages) == 0 {
		return
	}

	text := messages[0]
	if len(messages) > 1 {
		text = "🔔 Birden fazla alarm durumu değişti:\n"
		for _, m := range messages {
			text += "\n— " + m
		}
	}

	if err := e.notifier.SendAlert(ctx, text); err != nil {
		e.log.Warn("alarm bildirimi gonderilemedi", "hata", err.Error())
	}
}

func (e *Engine) queryValue(ctx context.Context, promQL string) (float64, bool) {
	data, err := e.prom.Query(ctx, promQL, time.Now())
	if err != nil {
		return 0, false
	}
	var parsed struct {
		Result []struct {
			Value []json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil || len(parsed.Result) == 0 || len(parsed.Result[0].Value) < 2 {
		return 0, false
	}
	var raw string
	if err := json.Unmarshal(parsed.Result[0].Value[1], &raw); err != nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(raw, 64)
	return value, err == nil
}

func compare(value float64, operator string, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	default:
		return false
	}
}

func formatAlertMessage(rule store.AlertRule, state string, value float64) string {
	if state == "firing" {
		return "🚨 ALARM: " + rule.Name + "\n\nDeğer eşiği aştı: " + strconv.FormatFloat(value, 'f', 2, 64) +
			" (" + rule.Operator + " " + strconv.FormatFloat(rule.Threshold, 'f', 2, 64) + ")"
	}
	return "✅ Düzeldi: " + rule.Name + "\n\nGüncel değer: " + strconv.FormatFloat(value, 'f', 2, 64)
}
