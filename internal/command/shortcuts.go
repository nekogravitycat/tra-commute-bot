package command

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nekogravitycat/tra-commute-bot/internal/adapter/telegram"
	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// maxShortcutTriggerLen bounds free-text trigger input. A trigger is meant
// to be a short mnemonic word (e.g. "home"), and keeping it well under
// Telegram's 64-byte callback_data limit is what lets shortcutsKeyboard put
// the trigger itself, rather than an index, into its delete button.
const maxShortcutTriggerLen = 20

// reservedTriggers are the words a Shortcut's trigger must not collide with:
// every bot command, with and without its leading slash, so neither typing
// "/setup" nor "setup" as a trigger can shadow the real command.
func reservedTriggers() map[string]bool {
	out := make(map[string]bool, len(botCommands)*2)
	for _, c := range botCommands {
		out[c.Command] = true
		out["/"+c.Command] = true
	}
	return out
}

// startShortcuts begins /shortcuts: any prior session is dropped, the same
// as /manage and /usualtrain, since this is its own resting view rather than
// a continuation of whatever the chat was doing before.
func (r *Router) startShortcuts(ctx context.Context, chatID int64) {
	r.clearSession(chatID)
	r.sendShortcutsList(ctx)
}

func shortcutsListText(shortcuts []domain.Shortcut) string {
	explain := "傳送觸發詞彙就會立即查詢那條路線目前的班次，不受限於固定的通知時間——適合像下班這種時間不固定、無法排通知的行程。"
	if len(shortcuts) == 0 {
		return fmt.Sprintf("<b>快捷查詢</b>\n\n目前尚未設定任何捷徑。\n\n%s", explain)
	}
	var b strings.Builder
	b.WriteString("<b>快捷查詢</b>\n\n目前：\n")
	for _, s := range shortcuts {
		fmt.Fprintf(&b, "%s：%s→%s\n", esc(s.Trigger), esc(s.OriginName), esc(s.DestinationName))
	}
	fmt.Fprintf(&b, "\n%s", explain)
	return b.String()
}

func (r *Router) sendShortcutsList(ctx context.Context) {
	list := r.loadSettings(ctx)
	r.sendKeyboard(ctx, shortcutsListText(list.Shortcuts), shortcutsKeyboard(list.Shortcuts))
}

// handleShortcutsCallback handles every button on /shortcuts' list: add,
// delete-one, and done. Like usualtrain.go's handleUsualTrainCallback it is
// self-contained — it always re-reads settings fresh rather than trusting
// stale session state, so it still works even after a session has expired.
func (r *Router) handleShortcutsCallback(ctx context.Context, chatID int64, cq telegram.CallbackQuery) {
	data := cq.Data
	switch {
	case data == cbShortcutAdd:
		r.answer(ctx, cq.ID, "")
		sess := newShortcutSession(chatID, time.Now())
		r.setSession(chatID, sess)
		r.askCurrentField(ctx, sess)

	case strings.HasPrefix(data, cbShortcutDel):
		trigger := strings.TrimPrefix(data, cbShortcutDel)
		r.answer(ctx, cq.ID, "")
		_, err := r.Actor.Do(ctx, func(cur domain.SettingsList) (domain.SettingsList, any) {
			next := cur.RemoveShortcut(trigger)
			return next, next
		})
		if err != nil {
			r.Log.Warn("remove shortcut failed", "err", err)
			r.send(ctx, "移除失敗，請稍後再試一次")
			return
		}
		r.sendShortcutsList(ctx)

	case data == cbShortcutDone:
		r.answer(ctx, cq.ID, "")
		r.clearSession(chatID)
		r.send(ctx, "好，隨時輸入 /shortcuts 可以再調整")

	default:
		r.answer(ctx, cq.ID, "")
	}
}

// handleShortcutTriggerText answers FieldShortcutTrigger, the add flow's
// first question.
func (r *Router) handleShortcutTriggerText(ctx context.Context, sess *Session, text string) {
	trigger := strings.TrimSpace(text)
	switch {
	case trigger == "":
		r.send(ctx, "觸發詞不能是空的，換一個？")
		return
	case utf8.RuneCountInString(trigger) > maxShortcutTriggerLen:
		r.send(ctx, fmt.Sprintf("觸發詞太長了，請用 %d 個字以內，換一個？", maxShortcutTriggerLen))
		return
	case reservedTriggers()[strings.ToLower(trigger)]:
		r.send(ctx, fmt.Sprintf("「%s」是保留的指令名稱，換一個？", esc(trigger)))
		return
	}
	list := r.loadSettings(ctx)
	if list.TriggerTaken(trigger) {
		r.send(ctx, fmt.Sprintf("「%s」已經是另一個捷徑的觸發詞了，換一個？", esc(trigger)))
		return
	}
	sess.ShortcutDraft.Trigger = trigger
	r.advance(ctx, sess)
}

// confirmShortcut is reached once /shortcuts' add flow has every field
// answered. Unlike /setup there is no confirmation card: a Shortcut is a
// lightweight lookup, not a commute rule, so it is written straight away —
// the same immediacy as /usualtrain's add.
func (r *Router) confirmShortcut(ctx context.Context, sess *Session) {
	draft := sess.ShortcutDraft
	list := r.loadSettings(ctx)
	if list.TriggerTaken(draft.Trigger) {
		r.send(ctx, fmt.Sprintf("「%s」剛好被別的捷徑用掉了，請輸入 /shortcuts 重新開始", esc(draft.Trigger)))
		r.clearSession(sess.ChatID)
		return
	}
	_, err := r.Actor.Do(ctx, func(cur domain.SettingsList) (domain.SettingsList, any) {
		next := cur.AddShortcut(draft)
		return next, next
	})
	if err != nil {
		r.Log.Warn("add shortcut failed", "err", err)
		r.send(ctx, "新增失敗，請稍後再試一次")
		return
	}
	r.clearSession(sess.ChatID)
	r.sendShortcutsList(ctx)
}

// tryShortcut matches text against every configured Shortcut and, on a hit,
// runs and delivers its board query. It reports whether it recognised the
// text at all, so the caller can fall back to its usual "not sure what you
// mean" message when it did not.
func (r *Router) tryShortcut(ctx context.Context, text string) bool {
	list := r.loadSettings(ctx)
	sc, ok := list.FindShortcutByTrigger(text)
	if !ok {
		return false
	}
	r.runShortcut(ctx, sc)
	return true
}

// runShortcut answers one shortcut trigger: a live board query, right now,
// for that Shortcut's route.
func (r *Router) runShortcut(ctx context.Context, sc domain.Shortcut) {
	res := r.Board.Query(ctx, sc.OriginID, sc.DestinationID, sc.Route(), time.Now())
	msg := r.Renderer.RenderBoard(res)
	r.send(ctx, msg.Text)
}
