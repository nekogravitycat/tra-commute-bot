package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nekogravitycat/tra-commute-bot/internal/usecase"
)

func TestSend(t *testing.T) {
	var gotPath string
	var gotBody sendMessageRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer srv.Close()

	n := New(Config{BotToken: "123:ABC", ChatID: "999", APIBase: srv.URL})
	err := n.Send(context.Background(), usecase.Message{Text: "🚆 通勤簡報", ParseMode: "HTML"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if want := "/bot123:ABC/sendMessage"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotBody.ChatID != "999" {
		t.Errorf("chat_id = %q, want 999", gotBody.ChatID)
	}
	if gotBody.Text != "🚆 通勤簡報" {
		t.Errorf("text = %q, want the message verbatim", gotBody.Text)
	}
	if gotBody.ParseMode != "HTML" {
		t.Errorf("parse_mode = %q, want HTML", gotBody.ParseMode)
	}
	// A link preview card would push the recommendation off the notification,
	// which is the one line the user actually reads at 07:50.
	if !gotBody.DisableWebPagePreview {
		t.Error("link previews should be disabled")
	}
}

// TestSendAPIError checks a Telegram-level rejection is reported. The API
// answers 200 with ok:false for several real failures, so the status code
// alone is not enough to conclude the message was delivered.
func TestSendAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"ok":false,"error_code":400,"description":"can't parse entities"}`))
	}))
	defer srv.Close()

	n := New(Config{BotToken: "t", ChatID: "c", APIBase: srv.URL})
	err := n.Send(context.Background(), usecase.Message{Text: "<broken", ParseMode: "HTML"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "can't parse entities") {
		t.Errorf("error %q should carry Telegram's own description", err)
	}
}

func TestSendOKFalseWithHTTP200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	defer srv.Close()

	n := New(Config{BotToken: "t", ChatID: "c", APIBase: srv.URL})
	if err := n.Send(context.Background(), usecase.Message{Text: "hi"}); err == nil {
		t.Error("HTTP 200 with ok:false must still be treated as a failure")
	}
}

func TestSendUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	base := srv.URL
	srv.Close() // nothing is listening any more

	n := New(Config{BotToken: "t", ChatID: "c", APIBase: base})
	if err := n.Send(context.Background(), usecase.Message{Text: "hi"}); err == nil {
		t.Error("expected a transport error")
	}
}

func TestSendCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	n := New(Config{BotToken: "t", ChatID: "c", APIBase: srv.URL})
	if err := n.Send(ctx, usecase.Message{Text: "hi"}); err == nil {
		t.Error("expected the cancelled context to abort the send")
	}
}

func TestNewDefaults(t *testing.T) {
	n := New(Config{BotToken: "t", ChatID: "c"})
	if n.cfg.APIBase != DefaultAPIBase {
		t.Errorf("api base = %q, want the default", n.cfg.APIBase)
	}
	if n.http == nil || n.http.Timeout <= 0 {
		t.Error("a client with no timeout could hang the 07:50 run indefinitely")
	}
}
