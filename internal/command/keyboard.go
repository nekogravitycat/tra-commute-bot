package command

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/adapter/telegram"
	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// Callback data prefixes. Kept short and namespaced so a stray callback from
// a message the bot sent a long time ago (or, in principle, a different
// flow) is unambiguous to route or safely ignore.
const (
	cbStation      = "st:"      // st:<index into Session.StationMatches>
	cbStationRetry = "st:retry" // re-ask the station question
	cbWeekday      = "wd:"      // wd:<mon|tue|wed|thu|fri|sat|sun>
	cbWeekdayDone  = "wd:done"
	cbSetupConfirm = "setup:confirm"
	cbSetupRestart = "setup:restart"
	cbAfterAnother = "after:another"
	cbAfterList    = "after:list"
	cbAfterDone    = "after:done"
	cbManagePick   = "mng:pick:" // mng:pick:<index into Session.ListNames>
	cbManageNew    = "mng:new"
	cbManageBack   = "mng:back"
	cbEditRoute    = "mng:edit:route"
	cbEditReady    = "mng:edit:ready"
	cbEditDeadline = "mng:edit:deadline"
	cbEditNotify   = "mng:edit:notify"
	cbEditEarly    = "mng:edit:early"
	cbEditName     = "mng:edit:name"
	cbDelete       = "mng:delete"
	cbDeleteYes    = "mng:delete:yes"
	cbDeleteNo     = "mng:delete:no"

	cbUsualTrainAdd  = "ut:add"  // prompts for a train number as free text
	cbUsualTrainDel  = "ut:del:" // ut:del:<train no>
	cbUsualTrainDone = "ut:done"
)

// weekdayOrder is the button layout order for subflow C (§10.4-C): 一 to 日.
var weekdayOrder = []time.Weekday{
	time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday,
}

var weekdayLabelZh = map[time.Weekday]string{
	time.Monday: "一", time.Tuesday: "二", time.Wednesday: "三", time.Thursday: "四",
	time.Friday: "五", time.Saturday: "六", time.Sunday: "日",
}

var weekdayCallbackCode = map[time.Weekday]string{
	time.Monday: "mon", time.Tuesday: "tue", time.Wednesday: "wed", time.Thursday: "thu",
	time.Friday: "fri", time.Saturday: "sat", time.Sunday: "sun",
}

var weekdayFromCode = map[string]time.Weekday{
	"mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday,
	"fri": time.Friday, "sat": time.Saturday, "sun": time.Sunday,
}

// weekdaysZh renders a weekday set in TRA-bulletin order (一二三四五六日),
// the compact form used in every card and list row.
func weekdaysZh(wds []time.Weekday) string {
	set := map[time.Weekday]bool{}
	for _, w := range wds {
		set[w] = true
	}
	var b strings.Builder
	for _, w := range weekdayOrder {
		if set[w] {
			b.WriteString(weekdayLabelZh[w])
		}
	}
	return b.String()
}

func row(buttons ...telegram.InlineKeyboardButton) []telegram.InlineKeyboardButton { return buttons }

func btn(text, data string) telegram.InlineKeyboardButton {
	return telegram.InlineKeyboardButton{Text: text, CallbackData: data}
}

// stationKeyboard lists subflow A's disambiguation candidates, one per row
// so a long bilingual name is never truncated by a multi-column layout.
func stationKeyboard(matches []domain.Station) telegram.InlineKeyboardMarkup {
	m := telegram.InlineKeyboardMarkup{}
	for i, s := range matches {
		label := s.NameZh
		if s.NameEn != "" {
			label = fmt.Sprintf("%s (%s)", s.NameZh, s.NameEn)
		}
		m.InlineKeyboard = append(m.InlineKeyboard, row(btn(label, cbStation+strconv.Itoa(i))))
	}
	m.InlineKeyboard = append(m.InlineKeyboard, row(btn("重新輸入", cbStationRetry)))
	return m
}

// weekdayKeyboard renders subflow C's picker (§10.4-C): a checkmark prefix
// on every day already selected, laid out 三+三+一+完成 to match the picture
// in the spec, with "完成" on its own row so it cannot be mistaken for a day.
func weekdayKeyboard(selected map[time.Weekday]bool) telegram.InlineKeyboardMarkup {
	label := func(w time.Weekday) string {
		if selected[w] {
			return "✓ " + weekdayLabelZh[w]
		}
		return weekdayLabelZh[w]
	}
	wdBtn := func(w time.Weekday) telegram.InlineKeyboardButton {
		return btn(label(w), cbWeekday+weekdayCallbackCode[w])
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		row(wdBtn(time.Monday), wdBtn(time.Tuesday), wdBtn(time.Wednesday)),
		row(wdBtn(time.Thursday), wdBtn(time.Friday), wdBtn(time.Saturday)),
		row(wdBtn(time.Sunday)),
		row(btn("完成", cbWeekdayDone)),
	}}
}

func setupConfirmKeyboard() telegram.InlineKeyboardMarkup {
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		row(btn("確認建立", cbSetupConfirm), btn("重新開始", cbSetupRestart)),
	}}
}

func afterSetupKeyboard() telegram.InlineKeyboardMarkup {
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		row(btn("➕ 建立另一條規則", cbAfterAnother)),
		row(btn("📋 查看所有規則", cbAfterList)),
		row(btn("✅ 完成", cbAfterDone)),
	}}
}

// manageListKeyboard lists every Schedule plus the "add another" shortcut
// (§10.6). Buttons carry an index rather than the name itself, so an
// arbitrarily long user-chosen name never risks Telegram's 64-byte
// callback_data limit.
func manageListKeyboard(list domain.SettingsList) telegram.InlineKeyboardMarkup {
	m := telegram.InlineKeyboardMarkup{}
	for i, s := range list.Schedules {
		label := fmt.Sprintf("%s：%s %s　%s→%s",
			s.Name, weekdaysZh(s.ScheduleWeekdays), s.ScheduleAt, s.OriginName, s.DestinationName)
		m.InlineKeyboard = append(m.InlineKeyboard, row(btn(label, cbManagePick+strconv.Itoa(i))))
	}
	m.InlineKeyboard = append(m.InlineKeyboard, row(btn("➕ 新增一條規則", cbManageNew)))
	return m
}

func manageCardKeyboard() telegram.InlineKeyboardMarkup {
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		row(btn("改路線", cbEditRoute), btn("改最早到站", cbEditReady), btn("改最晚抵達", cbEditDeadline)),
		row(btn("改通知時間", cbEditNotify), btn("改提早出門上限", cbEditEarly), btn("改名字", cbEditName)),
		row(btn("🗑 刪除此規則", cbDelete), btn("⬅ 返回列表", cbManageBack)),
	}}
}

func deleteConfirmKeyboard() telegram.InlineKeyboardMarkup {
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		row(btn("確認刪除", cbDeleteYes), btn("取消", cbDeleteNo)),
	}}
}

// usualTrainKeyboard lists every habitual train number as its own delete
// button — tapping one removes just that number — plus the add and done
// shortcuts. One row per train keeps a long number never truncated, the same
// reasoning as stationKeyboard.
func usualTrainKeyboard(nos []string) telegram.InlineKeyboardMarkup {
	m := telegram.InlineKeyboardMarkup{}
	for _, no := range nos {
		m.InlineKeyboard = append(m.InlineKeyboard, row(btn("🗑 "+no, cbUsualTrainDel+no)))
	}
	m.InlineKeyboard = append(m.InlineKeyboard,
		row(btn("➕ 新增常搭班次", cbUsualTrainAdd)),
		row(btn("✅ 完成", cbUsualTrainDone)),
	)
	return m
}
