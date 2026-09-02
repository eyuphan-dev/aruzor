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

// firingDebounce is how long a rule's condition has to stay breached before
// it is reported as firing.
//
// The uptime checker treats a single failed request as a stall, not an
// outage, and only pages the chat once the target has stayed unreachable
// for a sustained window — a design explained at length next to that
// checker's own threshold. This engine used to skip that discipline
// entirely: one Prometheus sample above a threshold was enough to send
// "🚨 ALARM" immediately, and the next sample dipping back below it sent
// "✅ Düzeldi" just as fast. A metric hovering near its threshold could fire
// and resolve on every single evaluation tick.
//
// Recovery is not debounced the same way: once a rule is genuinely firing,
// reporting that it recovered is good news worth sending as soon as it is
// true, the same asymmetry the uptime checker already applies.
const firingDebounce = 3 * time.Minute

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

// ruleInput is what decideRuleTransition needs, kept free of the database
// and the Prometheus client so the debounce policy can be tested directly
// against wall-clock scenarios instead of waiting on real ticks.
type ruleInput struct {
	lastState    string
	pendingState string
	pendingSince *time.Time
	firing       bool
	now          time.Time
}

// ruleTransition is what changed and what to persist. commit is false while
// a breach is still inside the debounce window — pendingState/pendingSince
// are written so the countdown survives a restart, but lastState and the
// history table are not touched yet.
type ruleTransition struct {
	commit       bool
	newState     string
	pendingState string
	pendingSince *time.Time
	notify       bool
}

// decideRuleTransition is the whole debounce policy, kept pure so it can be
// tested directly: given the rule's last committed state, what it is
// currently pending on, and whether the condition is breached right now,
// what should be written and reported.
func decideRuleTransition(in ruleInput) ruleTransition {
	candidate := "ok"
	if in.firing {
		candidate = "firing"
	}

	if candidate == in.lastState {
		// Back to (or still at) the committed state — any pending breach in
		// the other direction is stale and forgotten rather than left to
		// resume counting from an old timestamp on a later flap.
		if in.pendingState != "" {
			return ruleTransition{commit: false, pendingState: "", pendingSince: nil}
		}
		return ruleTransition{}
	}

	// Recovery is reported the moment it is true — see firingDebounce's
	// comment for why firing and resolving are treated asymmetrically.
	if candidate == "ok" {
		return ruleTransition{commit: true, newState: "ok", notify: true}
	}

	// A new breach, or a breach already being timed: start (or keep) the
	// clock, and only commit once it has run long enough.
	since := in.pendingSince
	if in.pendingState != candidate || since == nil {
		since = &in.now
	}
	if in.now.Sub(*since) < firingDebounce {
		return ruleTransition{commit: false, pendingState: candidate, pendingSince: since}
	}
	return ruleTransition{commit: true, newState: "firing", notify: true}
}

// evaluateRule returns the notification text for this rule's transition (if
// any occurred, was actually committed, and isn't snoozed), or "" otherwise.
func (e *Engine) evaluateRule(ctx context.Context, rule store.AlertRule) string {
	value, ok := e.queryValue(ctx, rule.PromQL)
	if !ok {
		return ""
	}

	t := decideRuleTransition(ruleInput{
		lastState:    rule.LastState,
		pendingState: rule.PendingState,
		pendingSince: rule.PendingSince,
		firing:       compare(value, rule.Operator, rule.Threshold),
		now:          time.Now(),
	})

	if !t.commit {
		if t.pendingState != rule.PendingState {
			if err := e.db.SetAlertRulePending(ctx, rule.ID, t.pendingState, t.pendingSince); err != nil {
				e.log.Error("alarm bekleme durumu kaydedilemedi", "hata", err.Error())
			}
		}
		return ""
	}

	if err := e.db.UpdateAlertRuleState(ctx, rule.ID, t.newState, time.Now()); err != nil {
		e.log.Error("alarm durumu guncellenemedi", "hata", err.Error())
		return ""
	}

	event := "resolved"
	if t.newState == "firing" {
		event = "fired"
	}
	if err := e.db.InsertAlertEvent(ctx, uuid.NewString(), rule.ID, rule.Name, event, value); err != nil {
		e.log.Error("alarm gecmisi kaydedilemedi", "hata", err.Error())
	}

	e.log.Info("alarm durumu degisti", "kural", rule.Name, "eski", rule.LastState, "yeni", t.newState, "deger", value)

	if !t.notify || (rule.SnoozedUntil != nil && rule.SnoozedUntil.After(time.Now())) {
		if rule.SnoozedUntil != nil && rule.SnoozedUntil.After(time.Now()) {
			e.log.Info("alarm susturulmus, bildirim atlandi", "kural", rule.Name, "susturma_bitis", rule.SnoozedUntil)
		}
		return ""
	}

	return formatAlertMessage(rule, t.newState, value)
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
