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

// maxStationChoices caps subflow A's disambiguation keyboard (§10.4-A) so a
// generic query (e.g. a single common character) cannot produce an
// unusably long list of buttons.
const maxStationChoices = 8

// askCurrentField sends the prompt for whatever field Session.Cursor now
// points at, or finishes the flow if every field already has an answer.
func (r *Router) askCurrentField(ctx context.Context, sess *Session) {
	f, ok := sess.currentField()
	if !ok {
		r.finishFlow(ctx, sess)
		return
	}
	switch f {
	case FieldName:
		r.send(ctx, "幫這條規則取個名字")
	case FieldOrigin:
		r.send(ctx, "起始站？請輸入站名關鍵字（中文、英文、或站碼皆可）")
	case FieldDestination:
		r.send(ctx, "目的地站？請輸入站名關鍵字（中文、英文、或站碼皆可）")
	case FieldReadyAt:
		r.send(ctx, fmt.Sprintf("最早幾點能到 %s？請輸入時刻，格式 HH:mm（例如 08:20）", sess.Draft.OriginName))
	case FieldDeadlineAt:
		r.send(ctx, fmt.Sprintf("最晚幾點要抵達 %s？請輸入時刻，格式 HH:mm（例如 09:10）", sess.Draft.DestinationName))
	case FieldWeekdays:
		r.askWeekdays(ctx, sess)
	case FieldNotifyAt:
		r.send(ctx, "幾點通知你？請輸入時刻，格式 HH:mm（例如 07:50）")
	case FieldMaxEarlyLeave:
		r.send(ctx, "遲到時最多能接受提早幾分鐘出門？（輸入數字，例如 15）")
	}
}

// advance moves to the next field, or finishes the flow once every field has
// an answer — Session.Cursor reaching len(Fields) *is* the "ready to finish"
// state, so no separate marker is needed.
func (r *Router) advance(ctx context.Context, sess *Session) {
	sess.Cursor++
	sess.UpdatedAt = time.Now()
	r.askCurrentField(ctx, sess)
}

// finishFlow is reached once every field in the current flow has an answer.
// A brand new Schedule (Editing == "") shows the §10.5 confirmation card
// instead of writing immediately, honouring invariant 1 (§10.2): nothing
// reaches settings.json until the user explicitly confirms. A /manage field
// edit (Editing != "") has no such ceremony — the Schedule was already
// complete before the edit and is still complete after it, so it applies
// straight away (§10.6).
func (r *Router) finishFlow(ctx context.Context, sess *Session) {
	if sess.Editing == "" {
		r.send(ctx, fmt.Sprintf("✅ 設定完成：%s\n%s", esc(sess.Draft.Name), card(sess.Draft)))
		r.sendKeyboard(ctx, "確認要建立這條規則嗎？", setupConfirmKeyboard())
		return
	}
	r.applyEdit(ctx, sess)
}

// handleFieldText answers whichever field is currently active with free-text
// input. It is a no-op if no field is active (e.g. a stray message while
// /manage's card or the weekday picker is on screen and waiting on buttons).
func (r *Router) handleFieldText(ctx context.Context, sess *Session, text string) {
	f, ok := sess.currentField()
	if !ok {
		return
	}
	switch f {
	case FieldName:
		r.handleNameText(ctx, sess, text)
	case FieldOrigin, FieldDestination:
		r.handleStationText(ctx, sess, f, text)
	case FieldReadyAt, FieldDeadlineAt:
		r.handleTimeText(ctx, sess, f, text)
	case FieldWeekdays:
		r.send(ctx, "請用上面的按鈕選擇星期，或輸入 /cancel 取消")
	case FieldNotifyAt:
		r.handleTimeText(ctx, sess, f, text)
	case FieldMaxEarlyLeave:
		r.handleMinutesText(ctx, sess, text)
	}
}

func (r *Router) handleNameText(ctx context.Context, sess *Session, text string) {
	name := strings.TrimSpace(text)
	if name == "" {
		r.send(ctx, "名字不能是空的，換一個？")
		return
	}
	list := r.loadSettings(ctx)
	if list.NameTaken(name, sess.Editing) {
		r.send(ctx, fmt.Sprintf("「%s」已經是另一條規則的名字了，換一個？", esc(name)))
		return
	}
	sess.Draft.Name = name
	r.advance(ctx, sess)
}

func (r *Router) handleStationText(ctx context.Context, sess *Session, f Field, text string) {
	matches := domain.MatchStations(r.Stations, text)
	switch {
	case len(matches) == 0:
		r.send(ctx, "找不到符合的車站，換個關鍵字試試？")
	case len(matches) == 1:
		r.setStation(sess, f, matches[0])
		r.advance(ctx, sess)
	default:
		if len(matches) > maxStationChoices {
			matches = matches[:maxStationChoices]
		}
		sess.StationMatches = matches
		r.sendKeyboard(ctx, "找到多筆符合的車站，請選擇：", stationKeyboard(matches))
	}
}

func (r *Router) setStation(sess *Session, f Field, s domain.Station) {
	switch f {
	case FieldOrigin:
		sess.Draft.OriginID, sess.Draft.OriginName = s.ID, s.NameZh
	case FieldDestination:
		sess.Draft.DestinationID, sess.Draft.DestinationName = s.ID, s.NameZh
	}
}

// minutesOf lets two TimeOfDay values be compared without adding a method to
// the domain package for what is purely a UI validation concern.
func minutesOf(t domain.TimeOfDay) int { return t.Hour*60 + t.Minute }

func (r *Router) handleTimeText(ctx context.Context, sess *Session, f Field, text string) {
	t, err := domain.ParseTimeOfDay(text)
	if err != nil {
		r.send(ctx, "時刻格式錯誤，請用 HH:mm（例如 08:20）再試一次")
		return
	}

	switch f {
	case FieldReadyAt:
		if sess.Draft.DeadlineAt != (domain.TimeOfDay{}) && minutesOf(t) >= minutesOf(sess.Draft.DeadlineAt) {
			r.send(ctx, fmt.Sprintf("最早到站 %s 晚於或等於最晚抵達 %s，請輸入更早的時刻", t, sess.Draft.DeadlineAt))
			return
		}
		sess.Draft.ReadyAt = t
	case FieldDeadlineAt:
		if sess.Draft.ReadyAt != (domain.TimeOfDay{}) && minutesOf(t) <= minutesOf(sess.Draft.ReadyAt) {
			r.send(ctx, fmt.Sprintf("最晚抵達 %s 早於或等於最早到站 %s，請輸入 %s 之後的時刻", t, sess.Draft.ReadyAt, sess.Draft.ReadyAt))
			return
		}
		sess.Draft.DeadlineAt = t
	case FieldNotifyAt:
		sess.Draft.ScheduleAt = t
	}
	r.advance(ctx, sess)
}

func (r *Router) handleMinutesText(ctx context.Context, sess *Session, text string) {
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || n <= 0 {
		r.send(ctx, "請輸入正整數分鐘數，例如 15")
		return
	}
	sess.Draft.MaxEarlyLeave = time.Duration(n) * time.Minute
	r.advance(ctx, sess)
}

// askWeekdays sends subflow C's picker (§10.4-C), pre-checking whatever the
// draft already holds — so editing just the notify time via "改通知時間"
// (which re-asks weekdays too, since the two are edited together) starts
// from the current selection rather than an empty one.
func (r *Router) askWeekdays(ctx context.Context, sess *Session) {
	sess.PickerSelected = map[time.Weekday]bool{}
	for _, w := range sess.Draft.ScheduleWeekdays {
		sess.PickerSelected[w] = true
	}
	sess.PickerMessageID = r.sendKeyboard(ctx, "選擇通知星期（可多選，完成後按下方按鈕）：", weekdayKeyboard(sess.PickerSelected))
}

func weekdaySetToSlice(set map[time.Weekday]bool) []time.Weekday {
	var out []time.Weekday
	for _, w := range weekdayOrder {
		if set[w] {
			out = append(out, w)
		}
	}
	return out
}

// handleFieldCallback routes a button press that belongs to whichever field
// is currently active (subflow A's station list, subflow C's weekday
// picker). It reports whether it recognised and handled the press, so the
// caller can fall back to a bare acknowledgement for anything stale.
func (r *Router) handleFieldCallback(ctx context.Context, sess *Session, cq telegram.CallbackQuery) bool {
	f, ok := sess.currentField()
	if !ok {
		return false
	}
	data := cq.Data

	switch f {
	case FieldOrigin, FieldDestination:
		switch {
		case data == cbStationRetry:
			r.answer(ctx, cq.ID, "")
			sess.StationMatches = nil
			r.askCurrentField(ctx, sess)
			return true
		case strings.HasPrefix(data, cbStation):
			i, err := strconv.Atoi(strings.TrimPrefix(data, cbStation))
			if err != nil || i < 0 || i >= len(sess.StationMatches) {
				r.answer(ctx, cq.ID, "這個選項已經過期了，請重新輸入站名")
				return true
			}
			r.answer(ctx, cq.ID, "")
			r.setStation(sess, f, sess.StationMatches[i])
			sess.StationMatches = nil
			r.advance(ctx, sess)
			return true
		}

	case FieldWeekdays:
		switch {
		case data == cbWeekdayDone:
			if len(sess.PickerSelected) == 0 {
				r.answer(ctx, cq.ID, "至少選一天")
				return true
			}
			r.answer(ctx, cq.ID, "")
			sess.Draft.ScheduleWeekdays = weekdaySetToSlice(sess.PickerSelected)
			sess.PickerSelected = nil
			r.advance(ctx, sess)
			return true
		case strings.HasPrefix(data, cbWeekday):
			wd, ok := weekdayFromCode[strings.TrimPrefix(data, cbWeekday)]
			if !ok {
				r.answer(ctx, cq.ID, "")
				return true
			}
			if sess.PickerSelected == nil {
				sess.PickerSelected = map[time.Weekday]bool{}
			}
			// Deleting rather than storing false keeps len(PickerSelected)
			// meaningful as "how many days are selected" — the done button's
			// "at least one day" check below reads that length directly, and
			// weekdaySetToSlice only wants the true entries anyway.
			if sess.PickerSelected[wd] {
				delete(sess.PickerSelected, wd)
			} else {
				sess.PickerSelected[wd] = true
			}
			r.answer(ctx, cq.ID, "")
			if err := r.Bot.EditMessageReplyMarkup(ctx, sess.PickerMessageID, weekdayKeyboard(sess.PickerSelected)); err != nil {
				r.Log.Warn("update weekday keyboard failed", "err", err)
			}
			return true
		}
	}
	return false
}
