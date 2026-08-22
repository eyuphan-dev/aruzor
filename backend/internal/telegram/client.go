// Package telegram provides a minimal Telegram Bot API client: sending
// messages with an inline "refresh" button, editing them in place when the
// button is pressed, and long-polling for those button presses. No webhook
// / public HTTPS endpoint is required, which keeps a private alert channel
// private.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	botToken   string
	httpClient *http.Client
}

func NewClient(botToken string) *Client {
	return &Client{
		botToken:   botToken,
		httpClient: &http.Client{Timeout: 40 * time.Second},
	}
}

func (c *Client) Enabled() bool {
	return c.botToken != ""
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type sendMessageRequest struct {
	ChatID      string                `json:"chat_id"`
	Text        string                `json:"text"`
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

type editMessageRequest struct {
	ChatID      string                `json:"chat_id"`
	MessageID   int64                 `json:"message_id"`
	Text        string                `json:"text"`
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

type answerCallbackRequest struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
}

type apiResponse struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Desc   string          `json:"description"`
}

type Message struct {
	MessageID int64 `json:"message_id"`
}

func (c *Client) SendMessage(ctx context.Context, chatID, text string, markup *InlineKeyboardMarkup) (*Message, error) {
	var msg Message
	if err := c.call(ctx, "sendMessage", sendMessageRequest{ChatID: chatID, Text: text, ReplyMarkup: markup}, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (c *Client) EditMessageText(ctx context.Context, chatID string, messageID int64, text string, markup *InlineKeyboardMarkup) error {
	return c.call(ctx, "editMessageText", editMessageRequest{ChatID: chatID, MessageID: messageID, Text: text, ReplyMarkup: markup}, nil)
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackID, text string) error {
	return c.call(ctx, "answerCallbackQuery", answerCallbackRequest{CallbackQueryID: callbackID, Text: text}, nil)
}

type Update struct {
	UpdateID      int64            `json:"update_id"`
	CallbackQuery *CallbackQuery   `json:"callback_query,omitempty"`
	Message       *IncomingMessage `json:"message,omitempty"`
}

type CallbackQuery struct {
	ID      string `json:"id"`
	Data    string `json:"data"`
	Message *struct {
		MessageID int64 `json:"message_id"`
		Chat      struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

type IncomingMessage struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
}

// GetUpdates long-polls Telegram for new updates (button presses and text
// commands) starting after offset. It blocks up to ~30s if there is nothing
// new.
func (c *Client) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	var updates []Update
	body := map[string]any{"offset": offset, "timeout": 30, "allowed_updates": []string{"callback_query", "message"}}
	if err := c.call(ctx, "getUpdates", body, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// GetMe returns the bot's own username, which is what lets it tell whether
// a "/command@somebot" in a group was addressed to it or to a different
// bot sharing the same chat.
func (c *Client) GetMe(ctx context.Context) (string, error) {
	var me struct {
		Username string `json:"username"`
	}
	if err := c.call(ctx, "getMe", map[string]any{}, &me); err != nil {
		return "", err
	}
	return me.Username, nil
}

// SetMyCommands registers the bot's slash-command menu (the list Telegram
// shows when a user taps the "/" button next to the chat input).
func (c *Client) SetMyCommands(ctx context.Context, commands []BotCommand) error {
	return c.call(ctx, "setMyCommands", map[string]any{"commands": commands}, nil)
}

func (c *Client) call(ctx context.Context, method string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", c.botToken, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram istegi basarisiz: %w", err)
	}
	defer resp.Body.Close()

	var parsed apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return fmt.Errorf("telegram yaniti okunamadi: %w", err)
	}
	if !parsed.OK {
		return fmt.Errorf("telegram hatasi: %s", parsed.Desc)
	}
	if out != nil && len(parsed.Result) > 0 {
		return json.Unmarshal(parsed.Result, out)
	}
	return nil
}
