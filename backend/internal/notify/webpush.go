package notify

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"

	"aruzor/internal/store"
)

// WebPushChannel delivers to every browser subscribed through the app's
// notification prompt — the phone gets an alert on its lock screen even
// with the tab, and Telegram, closed. Alert text only: a monitoring
// dashboard has no business sending a rich payload the browser would render
// unreviewed.
type WebPushChannel struct {
	db         *store.Store
	log        *slog.Logger
	vapidPub   string
	vapidPriv  string
	subscriber string
}

func NewWebPushChannel(db *store.Store, log *slog.Logger, vapidPub, vapidPriv, subscriber string) *WebPushChannel {
	return &WebPushChannel{db: db, log: log, vapidPub: vapidPub, vapidPriv: vapidPriv, subscriber: subscriber}
}

func (w *WebPushChannel) Name() string { return "push" }

func (w *WebPushChannel) SendAlert(ctx context.Context, text string) error {
	subs, err := w.db.ListPushSubscriptions(ctx)
	if err != nil {
		return err
	}

	payload, err := pushPayload(text)
	if err != nil {
		return err
	}

	for _, sub := range subs {
		resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
		}, &webpush.Options{
			Subscriber:      w.subscriber,
			VAPIDPublicKey:  w.vapidPub,
			VAPIDPrivateKey: w.vapidPriv,
			TTL:             60 * 60, // an hour is plenty for an alert to still be worth showing
		})
		if err != nil {
			w.log.Warn("push bildirimi gonderilemedi", "hata", err.Error())
			continue
		}
		resp.Body.Close()

		// 404/410 is the push service telling us this subscription is dead
		// (browser data cleared, extension uninstalled, permission revoked
		// on the OS side) — every future send would fail the same way, so
		// it is removed rather than retried forever.
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			if err := w.db.DeletePushSubscription(ctx, sub.Endpoint); err != nil {
				w.log.Warn("gecersiz push kaydi silinemedi", "hata", err.Error())
			}
		}
	}
	return nil
}

// pushPayload is the JSON the service worker's push handler reads. Title
// stays fixed — the text itself already says what happened, and a second
// line of chrome around it adds nothing on a lock screen.
func pushPayload(text string) ([]byte, error) {
	return json.Marshal(struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}{Title: "Aruzor", Body: text})
}
