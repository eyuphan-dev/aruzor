// Package notify fans a single alert out to whichever channels are
// configured — Telegram, browser push, a generic webhook — without the
// callers (the alert engine, the uptime checker) needing to know how many
// there are or which ones exist. Each channel failing is that channel's
// problem alone; one bad webhook URL must never cost a chat message.
package notify

import (
	"context"
	"log/slog"
)

// Channel is anything that can deliver an alert line of text.
type Channel interface {
	SendAlert(ctx context.Context, text string) error
}

// DigestChannel additionally accepts the daily health digest. Telegram is
// the only channel that implements it today — a push notification or a
// webhook firing once a day with a wall of text is not what either is for.
type DigestChannel interface {
	Channel
	SendDailyDigest(ctx context.Context) error
}

// Broadcaster satisfies both alerts.Notifier and uptime.Notifier by
// structural typing (SendAlert, and SendAlert+SendDailyDigest), so it can
// replace a lone *telegram.Bot everywhere one was passed directly.
type Broadcaster struct {
	channels []Channel
	log      *slog.Logger
}

func NewBroadcaster(log *slog.Logger, channels ...Channel) *Broadcaster {
	// Nil channels happen when a caller conditionally builds one (e.g. no
	// webhook URL configured) and appends the zero value; filtering here
	// keeps every call site simple.
	live := make([]Channel, 0, len(channels))
	for _, c := range channels {
		if c != nil {
			live = append(live, c)
		}
	}
	return &Broadcaster{channels: live, log: log}
}

// SendAlert delivers to every channel, independently. A failure on one is
// logged and does not stop the others — the whole point of having more than
// one channel is that they fail for unrelated reasons.
func (b *Broadcaster) SendAlert(ctx context.Context, text string) error {
	for _, ch := range b.channels {
		if err := ch.SendAlert(ctx, text); err != nil {
			b.log.Warn("bildirim kanali basarisiz", "kanal", channelName(ch), "hata", err.Error())
		}
	}
	return nil
}

// TestResult is one channel's outcome from SendTest.
type TestResult struct {
	Channel string `json:"channel"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

// SendTest sends one message through every configured channel and reports
// each one's own outcome, unlike SendAlert which fires-and-forgets and only
// logs failures. A settings page "send test notification" button exists
// specifically so a wrong webhook URL is caught the moment it is typed
// rather than the next time a real alert happens to fire — that only works
// if the button can say which channel failed and why.
func (b *Broadcaster) SendTest(ctx context.Context, text string) []TestResult {
	if len(b.channels) == 0 {
		return []TestResult{}
	}
	out := make([]TestResult, 0, len(b.channels))
	for _, ch := range b.channels {
		res := TestResult{Channel: channelName(ch)}
		if err := ch.SendAlert(ctx, text); err != nil {
			res.Error = err.Error()
		} else {
			res.OK = true
		}
		out = append(out, res)
	}
	return out
}

func (b *Broadcaster) SendDailyDigest(ctx context.Context) error {
	for _, ch := range b.channels {
		dc, ok := ch.(DigestChannel)
		if !ok {
			continue
		}
		if err := dc.SendDailyDigest(ctx); err != nil {
			b.log.Warn("gunluk ozet kanali basarisiz", "kanal", channelName(ch), "hata", err.Error())
		}
	}
	return nil
}

type named interface {
	Name() string
}

func channelName(ch Channel) string {
	if n, ok := ch.(named); ok {
		return n.Name()
	}
	return "bilinmeyen"
}
