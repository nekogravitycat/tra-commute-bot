package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Update is one item returned by getUpdates: either an incoming text message
// or a button press on a previously sent inline keyboard, never both.
type Update struct {
	UpdateID      int            `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

// Message is an incoming message (as opposed to usecase.Message, which is
// outgoing and carries no envelope fields).
type Message struct {
	MessageID int    `json:"message_id"`
	Text      string `json:"text"`
	Chat      Chat   `json:"chat"`
}

// Chat identifies the conversation a message belongs to.
type Chat struct {
	ID int64 `json:"id"`
}

// CallbackQuery is a press on an inline keyboard button. Data is whatever
// the button's CallbackData was; Message is the message the keyboard was
// attached to, so the handler can call EditMessageReplyMarkup on it.
type CallbackQuery struct {
	ID      string   `json:"id"`
	Data    string   `json:"data"`
	Message *Message `json:"message,omitempty"`
}

// InlineKeyboardButton is one button. CallbackData is opaque to Telegram and
// is round-tripped back verbatim in the CallbackQuery it produces — the
// command package encodes flow state into it (§10.4's weekday picker, station
// disambiguation lists, and so on).
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

// InlineKeyboardMarkup is a grid of buttons, one row per slice element.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type updatesResponse struct {
	OK          bool     `json:"ok"`
	Description string   `json:"description"`
	Result      []Update `json:"result"`
}

// GetUpdates long-polls for updates with update_id > offset, holding the
// connection open for up to timeoutSeconds waiting for one to arrive. The
// Bot API recommends 30-50 seconds (§5.2); this is the one call in the whole
// program that is expected to sit on the wire for a while, so it uses its
// own client timeout rather than the short one Send and the rest use.
func (n *Notifier) GetUpdates(ctx context.Context, offset, timeoutSeconds int) ([]Update, error) {
	payload, err := json.Marshal(struct {
		Offset         int      `json:"offset"`
		Timeout        int      `json:"timeout"`
		AllowedUpdates []string `json:"allowed_updates"`
	}{Offset: offset, Timeout: timeoutSeconds, AllowedUpdates: []string{"message", "callback_query"}})
	if err != nil {
		return nil, fmt.Errorf("telegram: encode getUpdates: %w", err)
	}

	client := &http.Client{Timeout: time.Duration(timeoutSeconds+10) * time.Second}
	body, err := n.callWithClient(ctx, client, "getUpdates", payload)
	if err != nil {
		return nil, err
	}

	var out updatesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("telegram: decode getUpdates: %w", err)
	}
	if !out.OK {
		return nil, fmt.Errorf("telegram: getUpdates: %s", out.Description)
	}
	return out.Result, nil
}

type sendKeyboardRequest struct {
	ChatID                string               `json:"chat_id"`
	Text                  string               `json:"text"`
	ParseMode             string               `json:"parse_mode,omitempty"`
	DisableWebPagePreview bool                 `json:"disable_web_page_preview"`
	ReplyMarkup           InlineKeyboardMarkup `json:"reply_markup"`
}

type sentMessageResult struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Result      struct {
		MessageID int `json:"message_id"`
	} `json:"result"`
}

// SendKeyboard sends text with an inline keyboard attached and returns the
// sent message's ID, needed later to update the keyboard in place (the
// weekday picker's checkmarks, §10.4-C) via EditMessageReplyMarkup.
func (n *Notifier) SendKeyboard(ctx context.Context, text string, markup InlineKeyboardMarkup) (int, error) {
	payload, err := json.Marshal(sendKeyboardRequest{
		ChatID:                n.cfg.ChatID,
		Text:                  text,
		ParseMode:             "HTML",
		DisableWebPagePreview: true,
		ReplyMarkup:           markup,
	})
	if err != nil {
		return 0, fmt.Errorf("telegram: encode sendMessage: %w", err)
	}
	body, err := n.call(ctx, "sendMessage", payload)
	if err != nil {
		return 0, err
	}
	var out sentMessageResult
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, fmt.Errorf("telegram: decode sendMessage: %w", err)
	}
	if !out.OK {
		return 0, fmt.Errorf("telegram: sendMessage: %s", out.Description)
	}
	return out.Result.MessageID, nil
}

// EditMessageReplyMarkup replaces the inline keyboard on an already-sent
// message, without touching its text. This is how the weekday picker
// (§10.4-C) toggles a checkmark without resending the whole prompt.
func (n *Notifier) EditMessageReplyMarkup(ctx context.Context, messageID int, markup InlineKeyboardMarkup) error {
	payload, err := json.Marshal(struct {
		ChatID      string               `json:"chat_id"`
		MessageID   int                  `json:"message_id"`
		ReplyMarkup InlineKeyboardMarkup `json:"reply_markup"`
	}{ChatID: n.cfg.ChatID, MessageID: messageID, ReplyMarkup: markup})
	if err != nil {
		return fmt.Errorf("telegram: encode editMessageReplyMarkup: %w", err)
	}
	return n.callExpectOK(ctx, "editMessageReplyMarkup", payload)
}

// BotCommand is one entry in the "/" menu Telegram shows in the chat's
// attachment tray, set via SetMyCommands. Command excludes the leading "/".
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// SetMyCommands registers the bot's command list with Telegram so typing "/"
// in the chat pops up the menu with each command's description, instead of
// leaving the user to guess or read /help. It only needs calling once at
// startup — Telegram persists the list server-side between runs.
func (n *Notifier) SetMyCommands(ctx context.Context, commands []BotCommand) error {
	payload, err := json.Marshal(struct {
		Commands []BotCommand `json:"commands"`
	}{Commands: commands})
	if err != nil {
		return fmt.Errorf("telegram: encode setMyCommands: %w", err)
	}
	return n.callExpectOK(ctx, "setMyCommands", payload)
}

// AnswerCallbackQuery acknowledges a button press. Telegram shows a loading
// spinner on the button until this is called (or ~30s pass), so every
// callback handler calls it — with a toast for user-facing feedback (a
// validation error, most often) or empty text to just clear the spinner.
func (n *Notifier) AnswerCallbackQuery(ctx context.Context, callbackQueryID, text string) error {
	payload, err := json.Marshal(struct {
		CallbackQueryID string `json:"callback_query_id"`
		Text            string `json:"text,omitempty"`
	}{CallbackQueryID: callbackQueryID, Text: text})
	if err != nil {
		return fmt.Errorf("telegram: encode answerCallbackQuery: %w", err)
	}
	return n.callExpectOK(ctx, "answerCallbackQuery", payload)
}

type okResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

// callExpectOK is for methods whose result carries no data worth decoding —
// only whether the call succeeded.
func (n *Notifier) callExpectOK(ctx context.Context, method string, payload []byte) error {
	body, err := n.call(ctx, method, payload)
	if err != nil {
		return err
	}
	var out okResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("telegram: decode %s: %w", method, err)
	}
	if !out.OK {
		return fmt.Errorf("telegram: %s: %s", method, out.Description)
	}
	return nil
}

func (n *Notifier) call(ctx context.Context, method string, payload []byte) ([]byte, error) {
	return n.callWithClient(ctx, n.http, method, payload)
}

// callWithClient posts a JSON payload to one Bot API method and returns the
// raw response body, HTTP transport errors aside. It does not itself
// validate the response's "ok" field — callers decode into their own result
// shape and check that themselves, since some (GetUpdates) need the payload
// even to report an error usefully.
func (n *Notifier) callWithClient(ctx context.Context, client *http.Client, method string, payload []byte) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/bot%s/%s", n.cfg.APIBase, n.cfg.BotToken, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("telegram: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram: %s: %w", method, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("telegram: %s: read response: %w", method, err)
	}
	return body, nil
}
