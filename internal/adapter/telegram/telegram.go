// Package telegram delivers rendered messages through the Bot API.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/usecase"
)

// DefaultAPIBase is the Bot API root.
const DefaultAPIBase = "https://api.telegram.org"

// Config holds the bot credentials and endpoint.
type Config struct {
	BotToken string
	ChatID   string
	APIBase  string
	Timeout  time.Duration
	HTTP     *http.Client
}

// Notifier sends messages to one chat.
type Notifier struct {
	cfg  Config
	http *http.Client
}

// New builds a notifier.
func New(cfg Config) *Notifier {
	if cfg.APIBase == "" {
		cfg.APIBase = DefaultAPIBase
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: cfg.Timeout}
	}
	return &Notifier{cfg: cfg, http: cfg.HTTP}
}

type sendMessageRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
	// The brief carries no links worth previewing, and a preview card would
	// push the recommendation off the notification.
	DisableWebPagePreview bool `json:"disable_web_page_preview"`
}

type sendMessageResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	ErrorCode   int    `json:"error_code"`
}

// Send posts one message. Retries are the caller's concern, so that the delay
// between attempts is decided in one place.
func (n *Notifier) Send(ctx context.Context, m usecase.Message) error {
	payload, err := json.Marshal(sendMessageRequest{
		ChatID:                n.cfg.ChatID,
		Text:                  m.Text,
		ParseMode:             m.ParseMode,
		DisableWebPagePreview: true,
	})
	if err != nil {
		return fmt.Errorf("telegram: encode: %w", err)
	}

	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", n.cfg.APIBase, n.cfg.BotToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("telegram: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.http.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var out sendMessageResponse
	// A body that will not decode is still a failure worth reporting with its
	// status code, so the decode error itself is not the interesting one.
	_ = json.Unmarshal(body, &out)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !out.OK {
		desc := out.Description
		if desc == "" {
			desc = string(body)
		}
		return fmt.Errorf("telegram: HTTP %d: %s", resp.StatusCode, desc)
	}
	return nil
}
