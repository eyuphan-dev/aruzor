// Package uptime periodically checks user-defined HTTP/TCP monitors and
// records their up/down history, independent of Prometheus — useful for
// watching a service that doesn't expose Prometheus metrics at all.
package uptime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"aruzor/internal/store"
)

const (
	tickInterval = 15 * time.Second
	// keep 90 days of check history — the SLA report shows a 24h/7d/30d/90d
	// breakdown plus a 90-day daily strip, both of which need the data to
	// actually reach back that far.
	historyMaxAge = 90 * 24 * time.Hour

	// outageThreshold is how long a service has to stay unreachable before
	// the chat is told.
	//
	// Measured in time, not in failed checks. A count cannot express what
	// matters here: two failures are two minutes on a monitor checked every
	// minute and two hours on one checked hourly. The earlier count of two
	// turned every brief stall into an outage announcement — one real site
	// produced seven "down" plus seven "recovered" messages in a single day
	// while remaining perfectly usable, and by the time anyone opened it the
	// stall was already over. A chat that cries wolf that often stops being
	// read, which costs far more than a few minutes of delay on a real one.
	//
	// Five minutes is roughly the line between a stall and an outage: below
	// it a visitor retries and gets in, above it the site is simply gone.
	outageThreshold = 5 * time.Minute

	// How close a TLS certificate has to be to expiring before the chat is
	// told.
	//
	// The monitor page has shown a certificate's remaining life all along,
	// but only to whoever went and looked at that particular monitor — which
	// nobody does on the one week of the year it matters. An expired
	// certificate takes a site down completely and is entirely preventable,
	// so it is worth an interruption.
	//
	// Seven days is enough to renew by hand on a working day without the
	// message arriving so early that it gets dismissed and forgotten. The
	// warning card in the app still appears at fourteen days: it costs
	// nothing to see it earlier, and interrupting somebody is a different
	// bar from showing them something.
	certWarningWindow = 7 * 24 * time.Hour
)

// Notifier is the alerting channel. Nil when Telegram isn't configured, in
// which case checks still run and history is still recorded — the results
// are simply only visible in the UI.
type Notifier interface {
	SendAlert(ctx context.Context, text string) error
}

type Checker struct {
	db       *store.Store
	notifier Notifier
	log      *slog.Logger
}

func NewChecker(db *store.Store, notifier Notifier, log *slog.Logger) *Checker {
	return &Checker{db: db, notifier: notifier, log: log}
}

// Run blocks until ctx is canceled, checking each monitor once its own
// interval has elapsed since its last check.
func (c *Checker) Run(ctx context.Context) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	pruneTicker := time.NewTicker(6 * time.Hour)
	defer pruneTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkDue(ctx)
		case <-pruneTicker.C:
			if err := c.db.PruneOldMonitorChecks(ctx, time.Now().Add(-historyMaxAge)); err != nil {
				c.log.Warn("eski izleme kayitlari silinemedi", "hata", err.Error())
			}
		}
	}
}

func (c *Checker) checkDue(ctx context.Context) {
	monitors, err := c.db.ListMonitors(ctx)
	if err != nil {
		c.log.Error("izleme listesi alinamadi", "hata", err.Error())
		return
	}

	now := time.Now()
	for _, m := range monitors {
		interval := time.Duration(m.IntervalSeconds) * time.Second
		if m.LastCheckedAt != nil && now.Sub(*m.LastCheckedAt) < interval {
			continue
		}
		go c.checkOne(ctx, m)
	}
}

func (c *Checker) checkOne(ctx context.Context, m store.Monitor) {
	if m.Type != "http" && m.Type != "tcp" {
		c.log.Warn("bilinmeyen izleme turu", "tur", m.Type)
		return
	}

	// The budget covers every retry, not one attempt, so a check that has
	// to try three times still finishes rather than being cut off halfway
	// and recorded as a failure it is not.
	checkCtx, cancel := context.WithTimeout(ctx, totalProbeBudget)
	defer cancel()

	cfg := httpCheckConfig{
		method:             m.Method,
		body:               m.RequestBody,
		contentType:        m.ContentType,
		expectedStatuses:   parseExpectedStatus(m.ExpectedStatus),
		expectBodyContains: m.ExpectBodyContains,
	}
	res := probe(checkCtx, m.Type, m.Target, cfg)

	var statusCode *int
	if res.statusCode != 0 {
		statusCode = &res.statusCode
	}
	check := store.MonitorCheck{
		ID:          uuid.NewString(),
		MonitorID:   m.ID,
		OK:          res.ok,
		Attempts:    res.attempts,
		LatencyMs:   res.latencyMs,
		ErrorClass:  res.errorClass,
		ErrorDetail: res.errorDetail,
		ConnectMs:   res.connectMs,
		TLSMs:       res.tlsMs,
		StatusCode:  statusCode,
		CheckedAt:   time.Now(),
	}
	if err := c.db.RecordMonitorCheck(ctx, check, res.certExpiry); err != nil {
		c.log.Error("izleme sonucu kaydedilemedi", "hata", err.Error())
	}

	c.updateAlertState(ctx, m, res)
	c.warnOnExpiringCert(ctx, m, res.certExpiry)
}

// warnOnExpiringCert announces a certificate that is about to run out.
//
// It is deliberately separate from the outage path: an expiring certificate
// is not a failure, the monitor is still green, and folding it into the
// up/down state machine would mean a healthy service reporting itself down.
func (c *Checker) warnOnExpiringCert(ctx context.Context, m store.Monitor, expiry *time.Time) {
	d := decideCertWarning(certInput{
		name:      m.Name,
		expiry:    expiry,
		warnedFor: m.CertWarnedFor,
		now:       time.Now(),
	})
	if !d.notify || c.notifier == nil {
		return
	}
	if m.SnoozedUntil != nil && m.SnoozedUntil.After(time.Now()) {
		// Not marked as warned, so the message goes out once the snooze
		// ends rather than being lost to a maintenance window.
		c.log.Info("izleme susturulmus, sertifika uyarisi atlandi", "izleme", m.Name)
		return
	}
	if err := c.notifier.SendAlert(ctx, d.message); err != nil {
		c.log.Warn("sertifika uyarisi gonderilemedi", "izleme", m.Name, "hata", err.Error())
		return
	}
	// Written only after the message actually left, so a failed send is
	// retried on the next check instead of being silently swallowed.
	if err := c.db.SetMonitorCertWarned(ctx, m.ID, *expiry); err != nil {
		c.log.Error("sertifika uyari durumu kaydedilemedi", "izleme", m.Name, "hata", err.Error())
	}
	c.log.Info("sertifika uyarisi gonderildi", "izleme", m.Name, "bitis", expiry)
}

type certInput struct {
	name      string
	expiry    *time.Time
	warnedFor *time.Time
	now       time.Time
}

type certDecision struct {
	notify  bool
	message string
}

// decideCertWarning is the whole certificate-warning policy, kept free of
// the database so it can be tested directly.
func decideCertWarning(in certInput) certDecision {
	// No certificate observed: a TCP monitor, a plain-HTTP target, or a
	// check that never got far enough to see one.
	if in.expiry == nil {
		return certDecision{}
	}
	remaining := in.expiry.Sub(in.now)
	if remaining > certWarningWindow {
		return certDecision{}
	}
	// Already expired is not a warning, it is an outage — and the probe
	// already reports it as one, with the certificate named as the cause.
	// Sending both would say the same thing twice in different words.
	if remaining <= 0 {
		return certDecision{}
	}
	// One message per certificate. A renewal changes the expiry date, which
	// is why the date is what gets remembered: the new certificate does not
	// match the warned-about one, so it can be warned about in its turn.
	if in.warnedFor != nil && in.warnedFor.Equal(*in.expiry) {
		return certDecision{}
	}

	return certDecision{
		notify: true,
		message: fmt.Sprintf(
			"🔐 %s sertifikasının süresi %s içinde doluyor.\n\nBitiş: %s\n\nYenilenmezse site tamamen erişilemez hale gelir.",
			in.name, humanCertRemaining(remaining), in.expiry.Local().Format("02.01.2006 15:04"),
		),
	}
}

// humanCertRemaining reads in days down to the last one, then in hours.
// "0 gün" on the final day would be both alarming and wrong.
func humanCertRemaining(d time.Duration) string {
	if days := int(d.Hours() / 24); days >= 1 {
		return fmt.Sprintf("%d gün", days)
	}
	hours := int(d.Hours())
	if hours < 1 {
		return "1 saatten az"
	}
	return fmt.Sprintf("%d saat", hours)
}

// updateAlertState decides whether this result changes what the chat has
// been told, and notifies only on that change. Nothing is sent while a
// monitor keeps passing, and nothing is repeated while it stays down — the
// message exists to mark the moment something crossed over.
func (c *Checker) updateAlertState(ctx context.Context, m store.Monitor, res probeResult) {
	d := decideAlertState(alertInput{
		state:        m.AlertState,
		failures:     m.ConsecutiveFailures,
		failingSince: m.FailingSince,
		ok:           res.ok,
		errorClass:   res.errorClass,
		name:         m.Name,
		now:          time.Now(),
	})
	state, failures, message := d.state, d.failures, d.message
	if message != "" && !res.ok {
		// The chat gets the one thing that is certain: what the network
		// stack reported. Everything else — what to go and check — lives in
		// the app, where it can be laid out as a list instead of a wall of
		// text in a notification.
		message += "\n\nSebep: " + causeLabel(res.errorClass, res.errorDetail)
	}

	if err := c.db.SetMonitorAlertState(ctx, m.ID, state, failures, d.failingSince); err != nil {
		c.log.Error("izleme uyari durumu guncellenemedi", "izleme", m.Name, "hata", err.Error())
		// Without the state written, the same message would go out again on
		// the next tick, so the notification is dropped with it.
		return
	}

	if message == "" || c.notifier == nil {
		return
	}
	if m.SnoozedUntil != nil && m.SnoozedUntil.After(time.Now()) {
		c.log.Info("izleme susturulmus, bildirim atlandi", "izleme", m.Name, "susturma_bitis", m.SnoozedUntil)
		return
	}
	if err := c.notifier.SendAlert(ctx, message); err != nil {
		c.log.Warn("izleme bildirimi gonderilemedi", "izleme", m.Name, "hata", err.Error())
		return
	}
	c.log.Info("izleme bildirimi gonderildi", "izleme", m.Name, "durum", state)
}

type alertInput struct {
	state        string
	failures     int
	failingSince *time.Time
	ok           bool
	errorClass   string
	name         string
	now          time.Time
}

type alertDecision struct {
	state        string
	failures     int
	failingSince *time.Time
	message      string
}

// pagesChat reports whether a failure class is allowed to reach the chat at
// all. A 403 or 429 means the site refused *us* — a WAF rule or a rate
// limit — which says nothing about whether it is serving everybody else.
// Announcing that as an outage would be plainly wrong, so it is recorded
// and shown in the app but never sent.
//
// ClassContentMismatch is deliberately left out of this exclusion — a page
// that answers but no longer says what it's configured to say (a broken
// form, a route silently swapped for a maintenance page) is exactly the
// class of real, user-facing failure this whole feature exists to catch.
func pagesChat(class string) bool {
	return class != ClassHTTPBlocked
}

// decideAlertState is the whole notification policy, kept free of the
// database so it can be tested directly: given what the chat was last told
// and how long the current run of failures has lasted, what should it be
// told now.
//
// Every check is recorded either way. This decides only what is worth
// interrupting somebody for, which is a far smaller set — the app is where
// you go to see that the site stalled at 15:05; the chat is for the times
// it is actually gone.
func decideAlertState(in alertInput) alertDecision {
	state := in.state
	// A monitor added before this column existed reads as empty, and an
	// empty state must mean "healthy" — treating it as anything else would
	// announce a recovery for a service that never went down.
	if state == "" {
		state = "ok"
	}

	if in.ok {
		d := alertDecision{state: "ok", failures: 0, failingSince: nil}
		if state == "down" {
			d.message = fmt.Sprintf("🟢 %s tekrar ayakta.", in.name)
			if in.failingSince != nil {
				d.message += fmt.Sprintf("\n\nKesinti süresi: %s.", humanDuration(in.now.Sub(*in.failingSince)))
			}
		}
		return d
	}

	// The clock starts at the first failure of this run and keeps running
	// across restarts, because it is stored on the monitor rather than held
	// in memory.
	failingSince := in.failingSince
	if failingSince == nil {
		since := in.now
		failingSince = &since
	}
	d := alertDecision{state: state, failures: in.failures + 1, failingSince: failingSince}

	down := in.now.Sub(*failingSince)
	if state != "down" && pagesChat(in.errorClass) && down >= outageThreshold {
		d.state = "down"
		d.message = fmt.Sprintf("🔴 %s erişilemiyor.\n\n%s süredir yanıt vermiyor.", in.name, humanDuration(down))
	}
	return d
}

// humanDuration keeps the message readable. "312 dakika" is a number a
// reader has to convert; "5 saat 12 dakika" is one they already understand.
func humanDuration(d time.Duration) string {
	minutes := int(d.Minutes())
	if minutes < 60 {
		return fmt.Sprintf("%d dakika", minutes)
	}
	hours := minutes / 60
	if rem := minutes % 60; rem > 0 {
		return fmt.Sprintf("%d saat %d dakika", hours, rem)
	}
	return fmt.Sprintf("%d saat", hours)
}
