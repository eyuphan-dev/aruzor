package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"aruzor/internal/store"
)

// WebhookChannel posts each alert as JSON to one operator-configured URL —
// Slack, Discord, n8n, a Zapier catch hook, or anything else that accepts a
// POST. There is no per-provider setup: Discord's incoming-webhook endpoint
// wants {"content": ...} instead of the more common {"text": ...}, so that
// one case is detected from the URL and everyone else gets the common shape,
// which Slack, Mattermost and most generic receivers already understand.
//
// The URL is read from settings on every send rather than captured once at
// startup, so changing it in the UI takes effect immediately — no restart,
// same as every other setting in Aruzor.
type WebhookChannel struct {
	db     *store.Store
	client *http.Client
}

func NewWebhookChannel(db *store.Store) *WebhookChannel {
	return &WebhookChannel{db: db, client: &http.Client{Timeout: 10 * time.Second}}
}

func (w *WebhookChannel) Name() string { return "webhook" }

func (w *WebhookChannel) SendAlert(ctx context.Context, text string) error {
	url, ok, err := w.db.GetSetting(ctx, "webhook_url")
	if err != nil {
		return err
	}
	if !ok || url == "" {
		return nil
	}

	var body map[string]string
	if strings.Contains(url, "discord.com/api/webhooks") {
		body = map[string]string{"content": text}
	} else {
		body = map[string]string{"text": text}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook %d dondurdu", resp.StatusCode)
	}
	return nil
}
