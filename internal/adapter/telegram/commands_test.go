package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetUpdates(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bott/getUpdates" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.Write([]byte(`{"ok":true,"result":[
			{"update_id":101,"message":{"message_id":5,"text":"/setup","chat":{"id":999}}},
			{"update_id":102,"callback_query":{"id":"cbq1","data":"wd:mon","message":{"message_id":6,"chat":{"id":999}}}}
		]}`))
	}))
	defer srv.Close()

	n := New(Config{BotToken: "t", ChatID: "999", APIBase: srv.URL})
	ups, err := n.GetUpdates(context.Background(), 100, 30)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(ups) != 2 {
		t.Fatalf("got %d updates, want 2", len(ups))
	}
	if ups[0].Message == nil || ups[0].Message.Text != "/setup" {
		t.Errorf("first update = %+v, want a /setup message", ups[0])
	}
	if ups[1].CallbackQuery == nil || ups[1].CallbackQuery.Data != "wd:mon" {
		t.Errorf("second update = %+v, want a wd:mon callback", ups[1])
	}
	if gotBody["offset"] != float64(100) {
		t.Errorf("offset = %v, want 100", gotBody["offset"])
	}
}

func TestGetUpdatesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
	}))
	defer srv.Close()

	n := New(Config{BotToken: "t", ChatID: "999", APIBase: srv.URL})
	if _, err := n.GetUpdates(context.Background(), 0, 1); err == nil {
		t.Error("expected an error for ok:false")
	}
}

func TestSendKeyboardReturnsMessageID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got sendKeyboardRequest
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		if len(got.ReplyMarkup.InlineKeyboard) != 1 || len(got.ReplyMarkup.InlineKeyboard[0]) != 2 {
			t.Errorf("keyboard = %+v, want a single row of two buttons", got.ReplyMarkup)
		}
		w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
	}))
	defer srv.Close()

	n := New(Config{BotToken: "t", ChatID: "999", APIBase: srv.URL})
	markup := InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "一", CallbackData: "wd:mon"}, {Text: "二", CallbackData: "wd:tue"}},
	}}
	id, err := n.SendKeyboard(context.Background(), "哪幾天通知你？", markup)
	if err != nil {
		t.Fatalf("SendKeyboard: %v", err)
	}
	if id != 42 {
		t.Errorf("message id = %d, want 42", id)
	}
}

func TestEditMessageReplyMarkup(t *testing.T) {
	var gotMessageID int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bott/editMessageReplyMarkup" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var got struct {
			MessageID int `json:"message_id"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		gotMessageID = got.MessageID
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	n := New(Config{BotToken: "t", ChatID: "999", APIBase: srv.URL})
	err := n.EditMessageReplyMarkup(context.Background(), 42, InlineKeyboardMarkup{})
	if err != nil {
		t.Fatalf("EditMessageReplyMarkup: %v", err)
	}
	if gotMessageID != 42 {
		t.Errorf("message_id = %d, want 42", gotMessageID)
	}
}

func TestAnswerCallbackQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bott/answerCallbackQuery" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	n := New(Config{BotToken: "t", ChatID: "999", APIBase: srv.URL})
	if err := n.AnswerCallbackQuery(context.Background(), "cbq1", "已更新"); err != nil {
		t.Fatalf("AnswerCallbackQuery: %v", err)
	}
}

func TestAnswerCallbackQueryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false,"description":"query is too old"}`))
	}))
	defer srv.Close()

	n := New(Config{BotToken: "t", ChatID: "999", APIBase: srv.URL})
	if err := n.AnswerCallbackQuery(context.Background(), "cbq1", ""); err == nil {
		t.Error("expected an error for ok:false")
	}
}
