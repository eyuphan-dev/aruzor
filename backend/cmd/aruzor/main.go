package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"strconv"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	webpush "github.com/SherClockHolmes/webpush-go"

	"aruzor/internal/alerts"
	"aruzor/internal/api"
	"aruzor/internal/auth"
	"aruzor/internal/notify"
	"aruzor/internal/prometheus"
	"aruzor/internal/store"
	"aruzor/internal/telegram"
	"aruzor/internal/traffic"
	"aruzor/internal/uptime"
)

func main() {
	// .env is optional and only for local development convenience — in
	// Docker/prod, env vars are passed directly and no .env file exists.
	_ = godotenv.Load()

	dbPath := envOr("ARUZOR_DB_PATH", "aruzor.db")
	addr := envOr("ARUZOR_LISTEN_ADDR", ":8080")
	corsOrigin := envOr("ARUZOR_CORS_ORIGIN", "http://localhost:3000")
	jwtSecret := os.Getenv("ARUZOR_JWT_SECRET")

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Left unset, the Prometheus address is probed rather than assumed, so
	// the same binary boots correctly on a bare server, inside compose, and
	// in a container next to a host-installed Prometheus.
	detectCtx, cancelDetect := context.WithTimeout(context.Background(), 10*time.Second)
	prometheusURL := prometheus.Detect(detectCtx, os.Getenv("ARUZOR_PROMETHEUS_URL"), logger)
	cancelDetect()

	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("veritabani acilamadi: %v", err)
	}
	defer db.Close()

	if jwtSecret == "" {
		jwtSecret = randomSecret()
		logger.Warn("ARUZOR_JWT_SECRET tanimli degil, gecici bir anahtar uretildi; sunucu her yeniden basladiginda mevcut oturumlar gecersiz olacak")
	}
	tokens := auth.NewTokenIssuer(jwtSecret, 24*time.Hour)

	if err := bootstrapSuperAdmin(db, logger); err != nil {
		log.Fatalf("super admin olusturulamadi: %v", err)
	}
	if err := bootstrapDefaultDatasource(db, prometheusURL, logger); err != nil {
		log.Fatalf("varsayilan veri kaynagi olusturulamadi: %v", err)
	}

	vapidCtx, cancelVapid := context.WithTimeout(context.Background(), 5*time.Second)
	vapidPub, vapidPriv, err := loadOrCreateVAPIDKeys(vapidCtx, db, logger)
	cancelVapid()
	if err != nil {
		log.Fatalf("push bildirim anahtari hazirlanamadi: %v", err)
	}

	promClient := prometheus.NewClient(prometheusURL)

	// Traffic analytics reads the web server's access log directly rather
	// than going through Prometheus — per-request facts (which IP, which
	// path, which user agent) simply are not in any exporter's output. With
	// no log configured and none found in the usual places the collector is
	// nil, the endpoints report the feature as off, and the page explains
	// how to switch it on.
	collector := traffic.NewCollector(db, logger, os.Getenv("ARUZOR_ACCESS_LOG_PATHS"))
	var trafficPaths []string
	if collector != nil {
		trafficPaths = collector.Sources()
	} else {
		logger.Info("erisim logu bulunamadi, trafik analizi kapali; ARUZOR_ACCESS_LOG_PATHS ile yol tanimlanabilir")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// One broadcaster feeds both the alert engine and the uptime checker, so
	// every channel (Telegram, browser push, the operator's webhook) hears
	// about a threshold breach and a service outage the same way. Built
	// before the router so the router can hand it to the settings page's
	// "send test notification" endpoint too.
	broadcaster := startAlertEngine(ctx, db, promClient, logger, vapidPub, vapidPriv)
	router := api.NewRouter(db, promClient, tokens, logger, corsOrigin, vapidPub, trafficPaths, broadcaster)

	go uptime.NewChecker(db, broadcaster, logger).Run(ctx)
	if collector != nil {
		go collector.Run(ctx)
	}

	server := &http.Server{Addr: addr, Handler: router}
	go func() {
		logger.Info("aruzor engine baslatildi", "adres", addr, "prometheus", prometheusURL)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("sunucu baslatilamadi: %v", err)
		}
	}()

	<-ctx.Done()
	logger.Info("kapatma sinyali alindi, sunucu durduruluyor")

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("sunucu duzgun kapatilamadi", "hata", err.Error())
	}
}

// startAlertEngine wires the alert evaluation engine to Telegram when
// ARUZOR_TELEGRAM_BOT_TOKEN/ARUZOR_TELEGRAM_CHAT_ID are set. Without them,
// rule state is still tracked but no notifications are sent.
// It returns the Telegram bot it built, or nil when Telegram isn't
// configured, so other background workers can notify through the same chat.
func startAlertEngine(ctx context.Context, db *store.Store, prom *prometheus.Client, logger *slog.Logger, vapidPub, vapidPriv string) *notify.Broadcaster {
	botToken := os.Getenv("ARUZOR_TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("ARUZOR_TELEGRAM_CHAT_ID")

	var bot *telegram.Bot
	if botToken != "" && chatID != "" {
		client := telegram.NewClient(botToken)
		bot = telegram.NewBot(client, chatID, prom, db, logger)
		// Development mode exposes hostnames, mount points, audit entries and
		// raw PromQL over Telegram. That is only ever acceptable for a private
		// bot the operator owns, so it must be switched on deliberately — and
		// it must never be set on the deployment whose bot posts to a shared
		// group.
		devMode := envOr("ARUZOR_TELEGRAM_DEV_MODE", "false") == "true"
		if devMode {
			bot.EnableDevMode()
		}
		bot.RegisterCommands(ctx)
		go bot.RunPolling(ctx)
		logger.Info("telegram bildirimleri aktif", "chat_id", chatID, "gelistirme_modu", devMode)
	} else {
		logger.Warn("ARUZOR_TELEGRAM_BOT_TOKEN / ARUZOR_TELEGRAM_CHAT_ID tanimli degil, alarm kurallari izlenecek ama bildirim gonderilmeyecek")
	}

	// The digest hour is a wall-clock hour in the process's local time. A
	// server left on UTC would send Turkey's 09:00 digest at noon, so pin the
	// zone explicitly instead of inheriting whatever the host happens to use.
	if name := envOr("ARUZOR_TIMEZONE", "Europe/Istanbul"); name != "" {
		loc, err := time.LoadLocation(name)
		if err != nil {
			logger.Warn("zaman dilimi yuklenemedi, sunucu saati kullanilacak", "zaman_dilimi", name, "hata", err.Error())
		} else {
			time.Local = loc
		}
	}

	digestHour, err := strconv.Atoi(envOr("ARUZOR_DAILY_DIGEST_HOUR", "9"))
	if err != nil || digestHour < 0 || digestHour > 23 {
		digestHour = 9
	}
	evalInterval, err := time.ParseDuration(envOr("ARUZOR_ALERT_EVAL_INTERVAL", "60s"))
	if err != nil {
		evalInterval = 60 * time.Second
	}

	// A nil *telegram.Bot handed to notify.Channel would be a non-nil
	// interface holding a nil pointer, so it is only added when there
	// actually is one — NewBroadcaster's own nil-filtering only catches a
	// literal nil interface, not this typed-nil case.
	var channels []notify.Channel
	if bot != nil {
		channels = append(channels, bot)
	}
	vapidSubject := envOr("ARUZOR_VAPID_SUBJECT", "mailto:admin@aruzor.local")
	channels = append(channels,
		notify.NewWebPushChannel(db, logger, vapidPub, vapidPriv, vapidSubject),
		notify.NewWebhookChannel(db),
	)
	broadcaster := notify.NewBroadcaster(logger, channels...)

	engine := alerts.NewEngine(db, prom, broadcaster, logger, evalInterval, digestHour)
	go engine.Run(ctx)

	return broadcaster
}

// bootstrapSuperAdmin creates the initial super_admin account from
// ARUZOR_ADMIN_EMAIL / ARUZOR_ADMIN_PASSWORD when the users table is empty.
// It exists for unattended installs; with no password configured the table
// is left empty on purpose and the UI runs its first-run setup instead.
func bootstrapSuperAdmin(db *store.Store, logger *slog.Logger) error {
	ctx := context.Background()
	count, err := db.CountUsers(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	password := os.Getenv("ARUZOR_ADMIN_PASSWORD")
	if password == "" {
		// Nothing to bootstrap from. The table stays empty and the UI shows
		// its first-run setup screen instead, which is both safer and fewer
		// steps than the old behaviour: generating a random password and
		// printing it to the log meant the first thing a new user had to do
		// was go and read container logs to find it.
		logger.Info("yonetici hesabi tanimli degil, ilk acilista kurulum ekrani gosterilecek")
		return nil
	}
	email := envOr("ARUZOR_ADMIN_EMAIL", "admin@aruzor.local")

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := db.CreateUser(ctx, uuid.NewString(), email, hash, "super_admin"); err != nil {
		return err
	}
	logger.Info("ilk super_admin kullanicisi olusturuldu", "email", email)
	return nil
}

// bootstrapDefaultDatasource creates the "default" Prometheus datasource
// (pointed at ARUZOR_PROMETHEUS_URL) when none exist yet, so a fresh
// install keeps working exactly as before multi-datasource support existed.
func bootstrapDefaultDatasource(db *store.Store, prometheusURL string, logger *slog.Logger) error {
	ctx := context.Background()
	count, err := db.CountDatasources(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	ds := store.Datasource{ID: "default", Name: "Varsayılan Prometheus", URL: prometheusURL, Type: "prometheus"}
	if err := db.CreateDatasource(ctx, ds); err != nil {
		return err
	}
	logger.Info("varsayilan veri kaynagi olusturuldu", "url", prometheusURL)
	return nil
}

// loadOrCreateVAPIDKeys returns the keypair that signs Web Push messages.
// Unlike the JWT secret, this cannot be regenerated on every restart — a
// browser's push subscription is bound to the public key it saw when the
// user granted permission, and a new keypair would silently orphan every
// existing subscription. So it is generated once and persisted in the
// settings table, the same place every other durable, non-secret-via-env
// piece of server state lives.
func loadOrCreateVAPIDKeys(ctx context.Context, db *store.Store, logger *slog.Logger) (pub, priv string, err error) {
	pub, pubOK, err := db.GetSetting(ctx, "vapid_public_key")
	if err != nil {
		return "", "", err
	}
	priv, privOK, err := db.GetSetting(ctx, "vapid_private_key")
	if err != nil {
		return "", "", err
	}
	if pubOK && privOK {
		return pub, priv, nil
	}

	priv, pub, err = webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", err
	}
	if err := db.SetSetting(ctx, "vapid_public_key", pub); err != nil {
		return "", "", err
	}
	if err := db.SetSetting(ctx, "vapid_private_key", priv); err != nil {
		return "", "", err
	}
	logger.Info("push bildirimleri icin yeni VAPID anahtari uretildi")
	return pub, priv, nil
}

func randomSecret() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
