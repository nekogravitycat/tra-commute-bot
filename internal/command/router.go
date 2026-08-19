package command

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/adapter/telegram"
	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
	"github.com/nekogravitycat/tra-commute-bot/internal/usecase"
)

// Router dispatches incoming Telegram updates to the §10.2-10.7 command and
// flow handlers. One Router serves the single configured chat — see
// isOurChat — for the process's whole lifetime; sessions live in its map for
// as long as a flow is in progress (§session.go).
type Router struct {
	Bot      *telegram.Notifier
	Actor    *usecase.SettingsActor
	State    usecase.StateStore
	Stations []domain.Station
	Log      *slog.Logger
	// ChatID is the single chat this bot answers to (TELEGRAM_CHAT_ID).
	// Anything from another chat is ignored outright: the spec is explicit
	// that this is a single-user system, and a stranger who finds the bot
	// must not be able to see or change anyone's commute settings.
	ChatID string

	// chatID is ChatID parsed once at construction, rather than formatting
	// every incoming update's int64 chat ID back to a string to compare —
	// isOurChat runs on every single message and callback the process ever
	// receives.
	chatID int64

	mu       sync.Mutex
	sessions map[int64]*Session
}

// NewRouter builds a Router. Bot, Actor, State, Stations and ChatID must all
// be set. chatID must parse as an int64 — the composition root gets it from
// config.Credentials.TelegramChatID, which config.LoadCredentials reads
// straight from the environment with no validation of its own, so a typo'd
// TELEGRAM_CHAT_ID surfaces here instead of as a router that silently
// answers no chat at all.
func NewRouter(bot *telegram.Notifier, actor *usecase.SettingsActor, state usecase.StateStore, stations []domain.Station, chatID string, log *slog.Logger) *Router {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		log.Error("TELEGRAM_CHAT_ID is not a valid chat ID, the bot will answer no chat", "value", chatID, "err", err)
	}
	return &Router{
		Bot: bot, Actor: actor, State: state, Stations: stations, ChatID: chatID, Log: log,
		chatID:   id,
		sessions: map[int64]*Session{},
	}
}

// botCommands is the "/" menu Telegram shows in the chat's attachment tray
// (via SetMyCommands) — kept in one place so it can't drift from the /help
// text in render.go's helpMessage.
var botCommands = []telegram.BotCommand{
	{Command: "setup", Description: "建立一條新的通勤規則"},
	{Command: "manage", Description: "查看、修改或刪除現有規則"},
	{Command: "usualtrain", Description: "管理常搭班次"},
	{Command: "status", Description: "查看每條規則的狀態"},
	{Command: "cancel", Description: "取消進行中的操作"},
	{Command: "help", Description: "顯示這份說明"},
}

// SetMyCommands registers botCommands with Telegram. Call once at startup;
// the list is persisted server-side, so this is not needed on every poll.
func (r *Router) SetMyCommands(ctx context.Context) error {
	return r.Bot.SetMyCommands(ctx, botCommands)
}

// HandleUpdate processes one update from getUpdates (§5.2 step 2).
func (r *Router) HandleUpdate(ctx context.Context, u telegram.Update) {
	switch {
	case u.Message != nil:
		r.handleMessage(ctx, *u.Message)
	case u.CallbackQuery != nil:
		r.handleCallback(ctx, *u.CallbackQuery)
	}
}

func (r *Router) isOurChat(id int64) bool {
	return id == r.chatID
}

func (r *Router) session(chatID int64) *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions[chatID]
}

func (r *Router) setSession(chatID int64, sess *Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[chatID] = sess
}

func (r *Router) clearSession(chatID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, chatID)
}

func (r *Router) handleMessage(ctx context.Context, msg telegram.Message) {
	if !r.isOurChat(msg.Chat.ID) {
		return
	}
	text := strings.TrimSpace(msg.Text)
	now := time.Now()

	switch text {
	case "/setup":
		r.startSetup(ctx, msg.Chat.ID, now)
		return
	case "/manage":
		r.startManage(ctx, msg.Chat.ID, now)
		return
	case "/usualtrain":
		r.startUsualTrain(ctx, msg.Chat.ID)
		return
	case "/status":
		r.handleStatus(ctx, msg.Chat.ID)
		return
	case "/help", "/start":
		r.handleHelp(ctx, msg.Chat.ID)
		return
	case "/cancel":
		r.handleCancel(ctx, msg.Chat.ID)
		return
	}
	if text == "" {
		return
	}

	sess := r.session(msg.Chat.ID)
	if sess.stale(now) {
		r.clearSession(msg.Chat.ID)
		r.send(ctx, "請用 /setup 或 /manage 開始，或輸入 /help 查看指令")
		return
	}
	if sess.AwaitingUsualTrainNo {
		sess.UpdatedAt = now
		r.handleUsualTrainText(ctx, sess, text)
		return
	}
	if _, ok := sess.currentField(); !ok {
		r.send(ctx, "請用上面的按鈕操作，或輸入 /cancel 取消")
		return
	}
	sess.UpdatedAt = now
	r.handleFieldText(ctx, sess, text)
}

func (r *Router) handleCallback(ctx context.Context, cq telegram.CallbackQuery) {
	if cq.Message == nil || !r.isOurChat(cq.Message.Chat.ID) {
		return
	}
	chatID := cq.Message.Chat.ID
	data := cq.Data

	// These four are self-contained navigation actions: they need no
	// in-progress flow state, only the chat to act on, so they work even
	// after a session has expired or been cleared (most notably right after
	// /setup's own confirmation clears its session, §10.7 point 1).
	switch {
	case strings.HasPrefix(data, "after:"):
		r.handleAfterCallback(ctx, chatID, cq)
		return
	case strings.HasPrefix(data, "ut:"):
		r.handleUsualTrainCallback(ctx, chatID, cq)
		return
	case data == cbManageNew:
		r.answer(ctx, cq.ID, "")
		r.startSetup(ctx, chatID, time.Now())
		return
	case data == cbManageBack:
		r.answer(ctx, cq.ID, "")
		r.startManage(ctx, chatID, time.Now())
		return
	case strings.HasPrefix(data, cbManagePick):
		r.answer(ctx, cq.ID, "")
		r.handleManagePick(ctx, chatID, data)
		return
	}

	now := time.Now()
	sess := r.session(chatID)
	if sess.stale(now) {
		r.answer(ctx, cq.ID, "這個操作已經過期了，請重新開始")
		r.clearSession(chatID)
		return
	}
	sess.UpdatedAt = now

	switch {
	case strings.HasPrefix(data, "setup:"):
		r.handleSetupCallback(ctx, sess, cq)
	case strings.HasPrefix(data, "mng:edit:"), data == cbDelete, strings.HasPrefix(data, "mng:delete:"):
		r.handleManageCardCallback(ctx, sess, cq)
	default:
		if !r.handleFieldCallback(ctx, sess, cq) {
			r.answer(ctx, cq.ID, "")
		}
	}
}

func (r *Router) handleStatus(ctx context.Context, _ int64) {
	list := r.loadSettings(ctx)
	state, err := r.State.Load()
	if err != nil {
		r.Log.Warn("state load failed", "err", err)
	}
	r.send(ctx, statusMessage(list, state, time.Now()))
}

func (r *Router) handleHelp(ctx context.Context, _ int64) {
	list := r.loadSettings(ctx)
	r.send(ctx, helpMessage(len(list.Schedules) > 0))
}

func (r *Router) handleCancel(ctx context.Context, chatID int64) {
	had := r.session(chatID) != nil
	r.clearSession(chatID)
	if had {
		r.send(ctx, "已取消，未做任何變更")
		return
	}
	r.send(ctx, "目前沒有進行中的操作")
}

// loadSettings is a read-only round trip through the settings actor
// (§10.9) — every read goes through the same serialized path as every
// write, so a read started right after a write always sees it.
func (r *Router) loadSettings(ctx context.Context) domain.SettingsList {
	res, err := r.Actor.Do(ctx, func(cur domain.SettingsList) (domain.SettingsList, any) { return cur, cur })
	if err != nil {
		r.Log.Warn("settings read failed", "err", err)
		return domain.SettingsList{}
	}
	return res.(domain.SettingsList)
}

func (r *Router) send(ctx context.Context, text string) {
	if err := r.Bot.Send(ctx, usecase.Message{Text: text, ParseMode: "HTML"}); err != nil {
		r.Log.Warn("send failed", "err", err)
	}
}

func (r *Router) sendKeyboard(ctx context.Context, text string, markup telegram.InlineKeyboardMarkup) int {
	id, err := r.Bot.SendKeyboard(ctx, text, markup)
	if err != nil {
		r.Log.Warn("send with keyboard failed", "err", err)
		return 0
	}
	return id
}

func (r *Router) answer(ctx context.Context, callbackQueryID, text string) {
	if err := r.Bot.AnswerCallbackQuery(ctx, callbackQueryID, text); err != nil {
		r.Log.Warn("answerCallbackQuery failed", "err", err)
	}
}
