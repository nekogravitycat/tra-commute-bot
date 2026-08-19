package command

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// esc escapes a value before it is interpolated into an HTML parse_mode
// message, so a schedule name containing "<" or "&" cannot break the
// rendering of everything after it. Station names come from the trusted static
// catalog and contain no such character, but they go through the same helper
// anyway: one rule for every interpolated value is easier to keep right than a
// per-field judgement about which source is trustworthy.
func esc(s string) string { return html.EscapeString(s) }

// cardRule is the separator line framing card()'s body top and bottom.
const cardRule = "──────────"

// card renders the summary shown at the end of /setup (§10.5) and at the top
// of a schedule's /manage view (§10.6) — the same shape both places, since
// they answer the same question: "what exactly is this rule, right now?" The
// name and any context-specific heading (e.g. "✅ 設定完成") are the caller's
// job, since the two call sites mean different things by it.
func card(s domain.Settings) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", cardRule)
	fmt.Fprintf(&b, "%s → %s\n", esc(s.OriginName), esc(s.DestinationName))
	fmt.Fprintf(&b, "通知：%s %s\n", weekdaysZh(s.ScheduleWeekdays), s.ScheduleAt)
	fmt.Fprintf(&b, "最早到站 %s　最晚抵達 %s\n", s.ReadyAt, s.DeadlineAt)
	fmt.Fprintf(&b, "提早出門上限 %d 分\n", int(s.MaxEarlyLeave/time.Minute))
	fmt.Fprintf(&b, "%s", cardRule)
	return b.String()
}

// fieldLabel names a field the way a human reading a diff or an error would
// say it, e.g. "deadline" rather than "DeadlineAt".
func fieldLabel(f Field) string {
	switch f {
	case FieldName:
		return "名字"
	case FieldOrigin:
		return "起始站"
	case FieldDestination:
		return "目的地站"
	case FieldReadyAt:
		return "最早到站"
	case FieldDeadlineAt:
		return "最晚抵達"
	case FieldWeekdays:
		return "通知星期"
	case FieldNotifyAt:
		return "通知時刻"
	case FieldMaxEarlyLeave:
		return "提早出門上限"
	default:
		return "設定"
	}
}

func fieldValue(f Field, s domain.Settings) string {
	switch f {
	case FieldName:
		return esc(s.Name)
	case FieldOrigin:
		return esc(s.OriginName)
	case FieldDestination:
		return esc(s.DestinationName)
	case FieldReadyAt:
		return s.ReadyAt.String()
	case FieldDeadlineAt:
		return s.DeadlineAt.String()
	case FieldWeekdays:
		return weekdaysZh(s.ScheduleWeekdays)
	case FieldNotifyAt:
		return s.ScheduleAt.String()
	case FieldMaxEarlyLeave:
		return fmt.Sprintf("%d 分", int(s.MaxEarlyLeave/time.Minute))
	default:
		return ""
	}
}

// diffMessage answers §10.7 point 5: "什麼改了、從什麼變成什麼", covering
// only the fields the just-finished flow actually touched.
func diffMessage(sess *Session) string {
	var lines []string
	for _, f := range sess.Fields {
		before := fieldValue(f, sess.Original)
		after := fieldValue(f, sess.Draft)
		if before == after {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s 已從 %s 改為 %s", fieldLabel(f), before, after))
	}
	if len(lines) == 0 {
		return "已更新，但這次輸入的值和原本相同"
	}
	return strings.Join(lines, "\n")
}

// listRow is one line of /status's per-schedule summary.
func listRow(s domain.Settings, st domain.TickState, now time.Time) string {
	return fmt.Sprintf("%s：%s %s　%s→%s\n  %s",
		esc(s.Name), weekdaysZh(s.ScheduleWeekdays), s.ScheduleAt,
		esc(s.OriginName), esc(s.DestinationName), todayStatus(s.Name, st, now))
}

// todayStatus says what the guard state records about one Schedule today.
// "Nothing recorded" is its own answer rather than a silent blank: with the
// notify loop running all day, a rule that is due and has no record at all is
// a different situation from one that tried and failed.
func todayStatus(name string, st domain.TickState, now time.Time) string {
	if st.SucceededOn(name, now) {
		return "今天已成功通知"
	}
	a := st.AttemptOn(name, now)
	switch {
	case a.Date == "":
		return "尚未有今天的紀錄"
	case a.GaveUp:
		return fmt.Sprintf("今天重試 %d 次後放棄", a.Count)
	default:
		return fmt.Sprintf("今天已嘗試 %d 次，尚未成功", a.Count)
	}
}

func statusMessage(list domain.SettingsList, state domain.TickState, now time.Time) string {
	if len(list.Schedules) == 0 {
		return "你還沒有設定任何通勤規則。用 /setup 建立第一條。"
	}
	var b strings.Builder
	b.WriteString("<b>目前的通勤規則</b>\n\n")
	for i, s := range list.Schedules {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(listRow(s, state, now))
	}
	return b.String()
}

func helpMessage(hasSchedules bool) string {
	if !hasSchedules {
		return "你還沒有設定任何通勤規則，用 /setup 建立第一條。\n\n" +
			"/setup 建立一條新的通勤規則\n" +
			"/manage 查看、修改或刪除現有規則\n" +
			"/usualtrain 管理常搭班次\n" +
			"/status 查看每條規則的狀態\n" +
			"/cancel 取消進行中的操作\n" +
			"/help 顯示這份說明"
	}
	return "<b>指令</b>\n\n" +
		"/setup 建立一條新的通勤規則\n" +
		"/manage 查看、修改或刪除現有規則\n" +
		"/usualtrain 管理常搭班次\n" +
		"/status 查看每條規則的狀態\n" +
		"/cancel 取消進行中的操作\n" +
		"/help 顯示這份說明"
}
