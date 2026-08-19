package command

import (
	"context"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/adapter/telegram"
	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// startSetup begins the §10.5 guided flow for a brand new Schedule,
// discarding whatever session — a stale flow, a /manage card, anything —
// the chat was previously in. /setup always starts clean; there is nothing
// worth preserving across an interruption this deliberate.
func (r *Router) startSetup(ctx context.Context, chatID int64, now time.Time) {
	sess := newSetupSession(chatID, now)
	r.setSession(chatID, sess)
	r.askCurrentField(ctx, sess)
}

// handleSetupCallback handles the confirmation card's two buttons
// (§10.5): 確認建立 writes the draft, 重新開始 discards it and starts over.
func (r *Router) handleSetupCallback(ctx context.Context, sess *Session, cq telegram.CallbackQuery) {
	switch cq.Data {
	case cbSetupConfirm:
		r.answer(ctx, cq.ID, "")
		r.confirmSetup(ctx, sess)
	case cbSetupRestart:
		r.answer(ctx, cq.ID, "")
		r.send(ctx, "已重新開始，剛才輸入的內容都不算數")
		r.startSetup(ctx, sess.ChatID, time.Now())
	default:
		r.answer(ctx, cq.ID, "")
	}
}

// confirmSetup is invariant 1 (§10.2) in code: this is the only place a
// brand new Schedule is ever written to settings.json, and it only runs once
// every field in setupFields has an answer.
func (r *Router) confirmSetup(ctx context.Context, sess *Session) {
	list := r.loadSettings(ctx)
	if list.NameTaken(sess.Draft.Name, "") {
		r.send(ctx, "「"+esc(sess.Draft.Name)+"」已經被使用了，請輸入 /setup 重新開始並換一個名字")
		r.clearSession(sess.ChatID)
		return
	}
	_, err := r.Actor.Do(ctx, func(cur domain.SettingsList) (domain.SettingsList, any) {
		next := cur.Upsert(sess.Draft)
		return next, next
	})
	if err != nil {
		r.Log.Warn("create schedule failed", "err", err)
		r.send(ctx, "建立失敗，請稍後再試一次")
		return
	}
	r.send(ctx, "已建立，會在下次符合條件時通知你")
	r.sendKeyboard(ctx, "接下來呢？", afterSetupKeyboard())
	r.clearSession(sess.ChatID)
}

// handleAfterCallback handles the §10.7 point 1 post-creation menu. It needs
// nothing from the session — only the chat to act on — which is what lets it
// still work after confirmSetup has already cleared it.
func (r *Router) handleAfterCallback(ctx context.Context, chatID int64, cq telegram.CallbackQuery) {
	switch cq.Data {
	case cbAfterAnother:
		r.answer(ctx, cq.ID, "")
		r.startSetup(ctx, chatID, time.Now())
	case cbAfterList:
		r.answer(ctx, cq.ID, "")
		r.startManage(ctx, chatID)
	case cbAfterDone:
		r.answer(ctx, cq.ID, "")
		r.send(ctx, "好，隨時輸入 /manage 可以再調整")
	default:
		r.answer(ctx, cq.ID, "")
	}
}
