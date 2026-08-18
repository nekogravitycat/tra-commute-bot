package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/adapter/telegram"
	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// maxTrainNoLen bounds free-text train-number input. TRA train numbers are a
// handful of digits (the longest in the catalog is four), so anything longer
// is almost certainly a typo rather than a real train number.
const maxTrainNoLen = 6

// startUsualTrain begins /usualtrain: any prior session is dropped, the same
// as /manage, since this is its own resting view rather than a continuation
// of whatever the chat was doing before.
func (r *Router) startUsualTrain(ctx context.Context, chatID int64) {
	r.clearSession(chatID)
	r.sendUsualTrainList(ctx)
}

// usualTrainListText explains what "usual" means before showing the list, so
// the delete buttons below it are not a mystery to a user who has forgotten
// why the bot is asking.
func usualTrainListText(nos []string) string {
	explain := "標記為「常搭」的班次即使被排序擠出候選名單，也一定會顯示在通知裡——看到「已過」或「誤點」是你要的資訊，表上找不到反而會被誤讀成程式壞了。"
	if len(nos) == 0 {
		return fmt.Sprintf("<b>常搭班次</b>\n\n目前尚未設定常搭班次。\n\n%s", explain)
	}
	return fmt.Sprintf("<b>常搭班次</b>\n\n目前：%s\n\n%s", escName(strings.Join(nos, "、")), explain)
}

func (r *Router) sendUsualTrainList(ctx context.Context) {
	list := r.loadSettings(ctx)
	r.sendKeyboard(ctx, usualTrainListText(list.UsualTrainNos), usualTrainKeyboard(list.UsualTrainNos))
}

// handleUsualTrainCallback handles every button on /usualtrain's list: add,
// delete-one, and done. Like the other resting-view callbacks in router.go
// (after:, mng:pick:, ...), it is self-contained — it always re-reads
// settings fresh rather than trusting session state, so it still works even
// after a session has expired.
func (r *Router) handleUsualTrainCallback(ctx context.Context, chatID int64, cq telegram.CallbackQuery) {
	data := cq.Data
	switch {
	case data == cbUsualTrainAdd:
		r.answer(ctx, cq.ID, "")
		r.setSession(chatID, &Session{ChatID: chatID, AwaitingUsualTrainNo: true, UpdatedAt: time.Now()})
		r.send(ctx, "輸入要新增的車次編號（例如 2008）")

	case strings.HasPrefix(data, cbUsualTrainDel):
		no := strings.TrimPrefix(data, cbUsualTrainDel)
		r.answer(ctx, cq.ID, "")
		_, err := r.Actor.Do(ctx, func(cur domain.SettingsList) (domain.SettingsList, any) {
			next := cur.RemoveUsualTrain(no)
			return next, next
		})
		if err != nil {
			r.Log.Warn("remove usual train failed", "err", err)
			r.send(ctx, "移除失敗，請稍後再試一次")
			return
		}
		r.sendUsualTrainList(ctx)

	case data == cbUsualTrainDone:
		r.answer(ctx, cq.ID, "")
		r.clearSession(chatID)
		r.send(ctx, "好，隨時輸入 /usualtrain 可以再調整")

	default:
		r.answer(ctx, cq.ID, "")
	}
}

// handleUsualTrainText answers the free-text prompt opened by "➕ 新增常搭班次".
func (r *Router) handleUsualTrainText(ctx context.Context, sess *Session, text string) {
	no, ok := parseTrainNo(text)
	if !ok {
		r.send(ctx, "車次編號格式不對，請輸入純數字（例如 2008）")
		return
	}

	_, err := r.Actor.Do(ctx, func(cur domain.SettingsList) (domain.SettingsList, any) {
		next := cur.AddUsualTrain(no)
		return next, next
	})
	if err != nil {
		r.Log.Warn("add usual train failed", "err", err)
		r.send(ctx, "新增失敗，請稍後再試一次")
		return
	}
	r.clearSession(sess.ChatID)
	r.sendUsualTrainList(ctx)
}

// parseTrainNo validates free-text input for a train number. TRA train
// numbers are plain digit strings (e.g. "2008"), so anything else is almost
// certainly a typo worth catching immediately rather than silently accepted
// and never matched against a real service.
func parseTrainNo(text string) (string, bool) {
	no := strings.TrimSpace(text)
	if no == "" || len(no) > maxTrainNoLen {
		return "", false
	}
	for _, c := range no {
		if c < '0' || c > '9' {
			return "", false
		}
	}
	return no, true
}
