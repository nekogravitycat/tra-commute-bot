package command

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/adapter/telegram"
	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// startManage shows the §10.6 schedule list. Any prior card/edit session is
// dropped: picking a schedule (mng:pick:<i>) re-reads the list fresh rather
// than trusting stale session state, so nothing needs to survive here.
func (r *Router) startManage(ctx context.Context, chatID int64, now time.Time) {
	r.clearSession(chatID)
	list := r.loadSettings(ctx)
	if len(list.Schedules) == 0 {
		r.send(ctx, "你還沒有設定任何通勤規則，用 /setup 建立第一條")
		return
	}
	r.sendKeyboard(ctx, "請選擇要管理的規則：", manageListKeyboard(list))
}

// handleManagePick resolves a list-row press to a Schedule and shows its
// card. The index is checked against a freshly loaded list rather than a
// cached one, so a schedule deleted or reordered between the list being
// shown and the button being pressed fails safely instead of opening the
// wrong card.
func (r *Router) handleManagePick(ctx context.Context, chatID int64, data string) {
	i, err := strconv.Atoi(strings.TrimPrefix(data, cbManagePick))
	if err != nil {
		return
	}
	list := r.loadSettings(ctx)
	if i < 0 || i >= len(list.Schedules) {
		r.send(ctx, "這個列表已經過期了，請重新輸入 /manage")
		return
	}
	r.sendManageCard(ctx, chatID, list.Schedules[i])
}

// sendManageCard shows one Schedule's summary and edit menu (§10.6), and
// opens a resting session for it — Fields is left nil, since no question is
// active yet, so a stray text message is recognised as "not answering
// anything right now" rather than being fed into some leftover field.
func (r *Router) sendManageCard(ctx context.Context, chatID int64, s domain.Settings) {
	sess := newEditSession(chatID, s, nil, time.Now())
	r.setSession(chatID, sess)
	r.sendKeyboard(ctx, card(s), manageCardKeyboard())
}

// handleManageCardCallback handles every button on a Schedule's card: the
// six field-edit shortcuts (each of which just sets up a mini collection
// flow reusing §10.4's subflows, per §10.2 invariant 2) and delete.
func (r *Router) handleManageCardCallback(ctx context.Context, sess *Session, cq telegram.CallbackQuery) {
	switch cq.Data {
	case cbEditRoute:
		r.answer(ctx, cq.ID, "")
		r.startFieldEdit(ctx, sess, FieldOrigin, FieldDestination)
	case cbEditReady:
		r.answer(ctx, cq.ID, "")
		r.startFieldEdit(ctx, sess, FieldReadyAt)
	case cbEditDeadline:
		r.answer(ctx, cq.ID, "")
		r.startFieldEdit(ctx, sess, FieldDeadlineAt)
	case cbEditNotify:
		r.answer(ctx, cq.ID, "")
		r.startFieldEdit(ctx, sess, FieldWeekdays, FieldNotifyAt)
	case cbEditEarly:
		r.answer(ctx, cq.ID, "")
		r.startFieldEdit(ctx, sess, FieldMaxEarlyLeave)
	case cbEditName:
		r.answer(ctx, cq.ID, "")
		r.startFieldEdit(ctx, sess, FieldName)
	case cbDelete:
		r.answer(ctx, cq.ID, "")
		r.sendDeleteConfirm(ctx, sess)
	case cbDeleteYes:
		r.answer(ctx, cq.ID, "")
		r.confirmDelete(ctx, sess)
	case cbDeleteNo:
		r.answer(ctx, cq.ID, "已取消刪除")
		r.sendManageCard(ctx, sess.ChatID, sess.Original)
	default:
		r.answer(ctx, cq.ID, "")
	}
}

func (r *Router) startFieldEdit(ctx context.Context, sess *Session, fields ...Field) {
	sess.Fields = fields
	sess.Cursor = 0
	r.askCurrentField(ctx, sess)
}

// applyEdit is reached once a /manage field edit's Fields are all answered.
// It writes the draft back — handling a rename by removing the old name
// first, since Upsert matches by the *new* name and would otherwise leave a
// duplicate — then reports what changed (§10.7 point 5) and returns to the
// (possibly renamed) card.
func (r *Router) applyEdit(ctx context.Context, sess *Session) {
	diff := diffMessage(sess)
	original, updated := sess.Original.Name, sess.Draft

	result, err := r.Actor.Do(ctx, func(cur domain.SettingsList) (domain.SettingsList, any) {
		next := cur
		if original != "" && original != updated.Name {
			next = next.Remove(original)
		}
		next = next.Upsert(updated)
		return next, next
	})
	if err != nil {
		r.Log.Warn("apply edit failed", "err", err)
		r.send(ctx, "更新失敗，請稍後再試一次")
		return
	}

	r.send(ctx, diff)
	list := result.(domain.SettingsList)
	if fresh, ok := list.Find(updated.Name); ok {
		r.sendManageCard(ctx, sess.ChatID, fresh)
		return
	}
	// Only reachable if the schedule was deleted by a concurrent /manage
	// session between the read and the write above.
	r.startManage(ctx, sess.ChatID, time.Now())
}

func (r *Router) sendDeleteConfirm(ctx context.Context, sess *Session) {
	state, err := r.State.Load()
	if err != nil {
		r.Log.Warn("state load failed", "err", err)
	}
	status := "尚未有今天的紀錄"
	if state.SucceededOn(sess.Original.Name, time.Now()) {
		status = "今天已成功通知"
	}
	text := fmt.Sprintf("確定要刪除「%s」嗎？（%s）\n這個動作無法復原。", escName(sess.Original.Name), status)
	r.sendKeyboard(ctx, text, deleteConfirmKeyboard())
}

func (r *Router) confirmDelete(ctx context.Context, sess *Session) {
	name := sess.Original.Name
	_, err := r.Actor.Do(ctx, func(cur domain.SettingsList) (domain.SettingsList, any) {
		next := cur.Remove(name)
		return next, next
	})
	if err != nil {
		r.Log.Warn("delete schedule failed", "err", err)
		r.send(ctx, "刪除失敗，請稍後再試一次")
		return
	}
	r.clearScheduleState(name)
	r.send(ctx, fmt.Sprintf("已刪除「%s」", escName(name)))
	r.startManage(ctx, sess.ChatID, time.Now())
}

// clearScheduleState wipes the deleted Schedule's guard history (§10.6): a
// name later reused by a new Schedule must not inherit "already delivered
// today" from a rule that no longer exists.
func (r *Router) clearScheduleState(name string) {
	st, err := r.State.Load()
	if err != nil {
		r.Log.Warn("state load failed", "err", err)
		return
	}
	delete(st.LastSuccess, name)
	delete(st.Attempts, name)
	if err := r.State.Save(st); err != nil {
		r.Log.Warn("clear schedule state failed", "name", name, "err", err)
	}
}
