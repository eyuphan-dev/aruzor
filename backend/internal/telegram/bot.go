package telegram

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"aruzor/internal/health"
	"aruzor/internal/prometheus"
	"aruzor/internal/store"
)

const refreshCallbackData = "refresh_health"

// commandCallbackPrefix namespaces menu-button callbacks so they can never
// be confused with the refresh button's fixed payload.
const commandCallbackPrefix = "cmd:"

var refreshKeyboard = &InlineKeyboardMarkup{
	InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "🔄 Güncelle", CallbackData: refreshCallbackData}},
	},
}

// botCommands is the bot's public slash-command menu (shown in Telegram's
// "/" button next to the chat input) and, by design, the *entire* set of
// things the bot will ever answer. Replies carry the same sanitized,
// aggregate-only data as the daily digest — never a hostname, IP, OS
// version, or any other detail that would help an attacker who somehow got
// access to this chat. There is intentionally no "server info" command.
//
// Kept deliberately short. /cpu, /bellek and /disk each returned a single
// line that /durum already contains, which tripled the menu for no new
// information; the room they took is now spent on the two things an
// operator actually reaches for — what is currently firing, and silencing
// it during maintenance.
var botCommands = []BotCommand{
	{Command: "durum", Description: "Sistem sağlığı: işlemci, bellek, disk"},
	{Command: "alarm", Description: "Şu anda alarm veren kurallar"},
	{Command: "izleme", Description: "İzlenen servisler ayakta mı"},
	{Command: "sustur", Description: "Bildirimleri sustur: /sustur 60 (dakika)"},
	{Command: "yardim", Description: "Komutları listele"},
}

// Bot wires the Telegram client to Aruzor's health summary: it can push a
// digest message with a "refresh" button, and answers button presses by
// editing that same message in place with a fresh, sanitized snapshot.
type Bot struct {
	client *Client
	chatID string
	prom   *prometheus.Client
	log    *slog.Logger
	// db backs /alarm and /sustur, and the development command set on top of
	// those. Nil only in tests.
	db      *store.Store
	devMode bool
	// username is this bot's own @name, learned at startup. Empty means the
	// lookup failed, in which case addressed commands are not filtered.
	username string
}

func NewBot(client *Client, chatID string, prom *prometheus.Client, db *store.Store, log *slog.Logger) *Bot {
	return &Bot{client: client, chatID: chatID, prom: prom, db: db, log: log}
}

// EnableDevMode unlocks the diagnostic command set. It is deliberately a
// separate call rather than a NewBot parameter so that turning it on is an
// explicit, greppable decision at the wiring site.
func (b *Bot) EnableDevMode() {
	b.devMode = true
}

func (b *Bot) SendDailyDigest(ctx context.Context) error {
	summary := health.Compute(ctx, b.prom)
	text := summary.Text("📊 Günlük Sistem Sağlığı")

	// Monitors ride along with the digest. An outage sends its own message
	// the moment it happens, so this is not the alerting path — it is the
	// daily confirmation that the services are still being watched and how
	// they did, which is the one thing silence cannot tell you.
	if line := b.monitorDigestLine(ctx); line != "" {
		text += "\n\n" + line
	}

	_, err := b.client.SendMessage(ctx, b.chatID, text, b.digestKeyboard())
	return err
}

func (b *Bot) Name() string { return "telegram" }

func (b *Bot) SendAlert(ctx context.Context, text string) error {
	// The menu rides along on alerts too: the first thing anyone does after
	// an alert is look at the rest of the system, and that shouldn't require
	// remembering a command.
	_, err := b.client.SendMessage(ctx, b.chatID, text, b.menuKeyboard())
	return err
}

// digestKeyboard puts the in-place refresh button above the command menu,
// so the digest can be re-read without posting a new message while still
// offering everything else one tap away.
func (b *Bot) digestKeyboard() *InlineKeyboardMarkup {
	menu := b.menuKeyboard()
	rows := append([][]InlineKeyboardButton{refreshKeyboard.InlineKeyboard[0]}, menu.InlineKeyboard...)
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

// RegisterCommands publishes the bot's "/" command menu to Telegram. Safe
// to call on every startup — it just overwrites the previous menu.
func (b *Bot) RegisterCommands(ctx context.Context) {
	// Learned here rather than in NewBot so construction stays free of
	// network calls. A failure is not fatal — the bot simply answers every
	// command it recognizes, which is the old behaviour.
	if name, err := b.client.GetMe(ctx); err != nil {
		b.log.Warn("telegram bot kimligi alinamadi", "hata", err.Error())
	} else {
		b.username = name
	}

	commands := botCommands
	if b.devEnabled() {
		commands = append(append([]BotCommand{}, botCommands...), devCommands...)
	}
	if err := b.client.SetMyCommands(ctx, commands); err != nil {
		b.log.Warn("telegram komut menusu kaydedilemedi", "hata", err.Error())
	}
}

// RunPolling long-polls Telegram for button presses until ctx is canceled.
// It never needs an inbound webhook, so no public HTTPS endpoint is required.
func (b *Bot) RunPolling(ctx context.Context) {
	var offset int64
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		updates, err := b.client.GetUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.log.Warn("telegram guncellemeleri alinamadi", "hata", err.Error())
			// Back off before retrying so a bad token or network outage
			// doesn't spin this loop at 100% CPU.
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		for _, u := range updates {
			offset = u.UpdateID + 1
			if u.CallbackQuery != nil {
				switch {
				case u.CallbackQuery.Data == refreshCallbackData:
					b.handleRefresh(ctx, u.CallbackQuery)
				case strings.HasPrefix(u.CallbackQuery.Data, commandCallbackPrefix):
					b.handleMenuButton(ctx, u.CallbackQuery)
				}
			}
			if u.Message != nil {
				b.handleMessage(ctx, u.Message)
			}
		}
	}
}

// handleMessage answers slash commands. Same chat-scoping rule as
// handleRefresh: only the configured admin chat gets a reply — if the bot's
// username is ever discovered and it ends up in another chat or gets a
// direct message, it stays silent instead of leaking anything.
func (b *Bot) handleMessage(ctx context.Context, msg *IncomingMessage) {
	if strconv.FormatInt(msg.Chat.ID, 10) != b.chatID {
		b.log.Warn("telegram mesaji beklenmeyen sohbetten geldi, yok sayildi")
		return
	}

	parts := strings.SplitN(strings.TrimSpace(msg.Text), " ", 2)
	var args string
	if len(parts) > 1 {
		args = parts[1]
	}

	// A group can hold several bots, and Telegram delivers an unaddressed
	// "/command" to all of them. "/command@somebot" exists precisely to pick
	// one — so a command addressed to a *different* bot must be ignored here,
	// otherwise both bots answer and the @-suffix means nothing.
	command, addressee, _ := strings.Cut(parts[0], "@")
	if addressee != "" && b.username != "" && !strings.EqualFold(addressee, b.username) {
		return
	}

	reply, ok := b.runCommand(ctx, command, args)
	if !ok {
		return // not a recognized command — stay quiet, don't echo unrelated chat
	}
	// Logged so "the bot didn't answer" can be told apart from "the message
	// never reached the bot" — in a group with more than one bot, or with
	// Telegram's privacy mode on, the second case is the common one.
	b.log.Info("telegram komutu isleniyor", "komut", command)
	b.reply(ctx, reply, b.menuKeyboard())
}

// runCommand resolves a command to its reply text. Both typed commands and
// menu button presses go through here, so a button can never drift out of
// sync with the command it stands for.
func (b *Bot) runCommand(ctx context.Context, command, args string) (string, bool) {
	if reply, handled := b.handleDevCommand(ctx, command, args); handled {
		return reply, true
	}
	switch command {
	case "/durum":
		return health.Compute(ctx, b.prom).Text("📊 Güncel Sistem Sağlığı"), true
	case "/alarm":
		return b.firingAlertsText(ctx), true
	case "/izleme":
		return b.monitorsText(ctx), true
	case "/sustur":
		return b.snoozeText(ctx, args), true
	case "/yardim", "/start":
		return b.helpText(), true
	}
	return "", false
}

func (b *Bot) reply(ctx context.Context, text string, keyboard *InlineKeyboardMarkup) {
	if _, err := b.client.SendMessage(ctx, b.chatID, text, keyboard); err != nil {
		b.log.Warn("telegram komut yaniti gonderilemedi", "hata", err.Error())
	}
}

// menuKeyboard renders every command as a tappable button. Telegram's "/"
// menu still works, but typing a command from memory is a poor first
// experience for anyone who didn't build the bot — and this same bot is
// meant to be dropped into other projects.
func (b *Bot) menuKeyboard() *InlineKeyboardMarkup {
	commands := botCommands
	if b.devEnabled() {
		commands = append(append([]BotCommand{}, botCommands...), devCommands...)
	}

	rows := [][]InlineKeyboardButton{}
	var row []InlineKeyboardButton
	for _, c := range commands {
		// /sorgu needs a PromQL argument, so a bare button would only ever
		// print its own usage text. It stays typed-only.
		if c.Command == "sorgu" {
			continue
		}
		row = append(row, InlineKeyboardButton{
			Text:         buttonLabels[c.Command],
			CallbackData: commandCallbackPrefix + c.Command,
		})
		if len(row) == 2 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

// buttonLabels are short, icon-led names — a button has far less room than
// the "/" menu's description line, and a wrapped label looks broken.
var buttonLabels = map[string]string{
	"durum":    "📊 Durum",
	"alarm":    "🔔 Alarmlar",
	"izleme":   "📡 İzlemeler",
	"sustur":   "🔕 1 Saat Sustur",
	"yardim":   "❓ Yardım",
	"detay":    "🔧 Detay",
	"hedefler": "🎯 Hedefler",
	"alarmlar": "📋 Kural Detayı",
	"loglar":   "📜 Loglar",
	"surum":    "⚙️ Süreç",
}

func (b *Bot) helpText() string {
	lines := "🤖 Aşağıdaki düğmelerden seçebilirsin — ya da komutu yazabilirsin:\n\n"
	for _, c := range botCommands {
		lines += "/" + c.Command + " — " + c.Description + "\n"
	}
	if b.devEnabled() {
		lines += "\n🔧 Geliştirme komutları:\n\n"
		for _, c := range devCommands {
			lines += "/" + c.Command + " — " + c.Description + "\n"
		}
	}
	return lines
}

// handleMenuButton runs the command behind a tapped button and posts the
// answer as a new message, keeping the menu attached so the user can carry
// on tapping. Same chat-scoping check as every other inbound handler.
func (b *Bot) handleMenuButton(ctx context.Context, cb *CallbackQuery) {
	if cb.Message == nil || strconv.FormatInt(cb.Message.Chat.ID, 10) != b.chatID {
		b.log.Warn("telegram callback beklenmeyen sohbetten geldi, yok sayildi")
		return
	}

	name := strings.TrimPrefix(cb.Data, commandCallbackPrefix)
	b.log.Info("telegram dugmesine basildi", "komut", name)
	reply, ok := b.runCommand(ctx, "/"+name, "")
	if !ok {
		// A button for a command this bot doesn't serve — most likely a menu
		// from before dev mode was switched off. Say so instead of leaving
		// the button spinning.
		reply = "Bu komut artık kullanılamıyor."
	}

	b.reply(ctx, reply, b.menuKeyboard())
	if err := b.client.AnswerCallbackQuery(ctx, cb.ID, ""); err != nil {
		b.log.Warn("telegram callback yanitlanamadi", "hata", err.Error())
	}
}

func (b *Bot) handleRefresh(ctx context.Context, cb *CallbackQuery) {
	// The bot only ever sends its refresh keyboard to the configured admin
	// chat, but if the bot's username is ever discovered and it gets added
	// to another chat, a callback query from that chat should never be
	// allowed to trigger an edit — defense in depth against chat spoofing.
	if cb.Message == nil || strconv.FormatInt(cb.Message.Chat.ID, 10) != b.chatID {
		b.log.Warn("telegram callback beklenmeyen sohbetten geldi, yok sayildi")
		return
	}

	summary := health.Compute(ctx, b.prom)
	if err := b.client.EditMessageText(ctx, b.chatID, cb.Message.MessageID, summary.Text("📊 Güncel Sistem Sağlığı"), b.digestKeyboard()); err != nil {
		b.log.Warn("telegram mesaji guncellenemedi", "hata", err.Error())
	}
	if err := b.client.AnswerCallbackQuery(ctx, cb.ID, "Güncellendi"); err != nil {
		b.log.Warn("telegram callback yanitlanamadi", "hata", err.Error())
	}
}
