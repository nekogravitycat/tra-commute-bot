// Package render turns a domain brief into the Telegram message described in
// §9 of the specification.
//
// It performs no business logic. Every number it prints was decided by the
// domain; this package only chooses the template and lays the numbers out.
//
// The message carries no emoji. Emphasis comes from bold headings and the
// structure of the text, and the comparison table marks each row with a
// single ASCII symbol explained by a legend underneath.
//
// Inside the <pre> block everything is ASCII, including the column headings,
// which are English abbreviations. Telegram's monospace font does not render
// a Chinese glyph at exactly twice the width of a Latin one, so mixing the two
// in an aligned grid produces a table that is crooked on the handset however
// carefully its widths are computed. Chinese goes in the prose around the
// table instead, where nothing is being aligned.
package render

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
	"github.com/nekogravitycat/tra-commute-bot/internal/usecase"
)

// Telegram is the HTML renderer for Telegram. HTML is used rather than
// Markdown because the comparison table needs a <pre> block to hold its
// columns, and because bold is the only emphasis this message needs.
type Telegram struct {
	// MaxAlternatives caps the rows in the comparison table.
	MaxAlternatives int
	// CertificateNote is the config's reminder of where to apply.
	CertificateNote string
}

// Row status labels, shown in the table's final column.
//
// They are words rather than symbols, and they trail the row rather than lead
// it, because of a rendering detail that cost a round of debugging: Telegram
// strips leading whitespace from a line even inside a <pre> block. A leading
// marker column whose "nothing to report" value was a blank therefore lost the
// indent on exactly those rows, knocking them out of alignment while every
// marked row stayed put.
//
// Putting the labels last means the first column is the train number, so no
// line can ever begin with a space. Trailing blanks are trimmed, which is
// harmless. TestPreLinesDoNotStartWithSpace guards the invariant.
const (
	statusRecommended = "REC"
	statusRisky       = "RISK"
	statusLate        = "LATE"
	statusMissed      = "GONE"
	statusCatchable   = "OK"
)

var weekdayZh = [...]string{"日", "一", "二", "三", "四", "五", "六"}

// Render produces the message for the brief's mode.
func (t Telegram) Render(b domain.Brief) usecase.Message {
	var body string
	switch b.Mode {
	case domain.ModeDegraded:
		body = t.renderDegraded(b)
	case domain.ModeNoService:
		body = t.renderNoService(b)
	case domain.ModeLate, domain.ModeSevere:
		body = t.renderLate(b)
	default:
		body = t.renderNormal(b)
	}
	return usecase.Message{Text: strings.TrimRight(body, "\n"), ParseMode: "HTML"}
}

// ---------------------------------------------------------------- normal (§9.1)

func (t Telegram) renderNormal(b domain.Brief) string {
	var s strings.Builder
	rec := *b.Plan.Recommended

	fmt.Fprintf(&s, "%s · %s\n\n", bold("通勤簡報"), t.header(b.GeneratedAt))

	fmt.Fprintf(&s, "%s\n", bold("建議搭乘 "+rec.TrainNo+" "+rec.TypeName))
	fmt.Fprintf(&s, "%s %s → %s %s\n",
		esc(b.Route.OriginName), clock(rec.EstDep),
		esc(b.Route.DestinationName), clock(rec.EstArr))
	fmt.Fprintf(&s, "%s · 預計 %s 抵達（餘裕 %d 分）\n",
		delayPhrase(rec), clock(rec.EstArr), minutes(b.Params.SlackFor(rec.EstArr)))

	if warn := unknownTypeWarning(b.Plan); warn != "" {
		s.WriteString("\n" + warn + "\n")
	}
	if b.Plan.BestRisky != nil {
		s.WriteString("\n" + t.riskyNote(b, *b.Plan.BestRisky) + "\n")
	}

	fmt.Fprintf(&s, "\n%s\n", bold("其他選項"))
	s.WriteString(pre(t.candidateTable(b)))
	s.WriteString(t.footer(b))
	return s.String()
}

// riskyNote surfaces the train that is catchable only if the user is prompt.
// The system does not gamble on the user's behalf, but it does not hide the
// option either — the delay it depends on could shrink at any moment, and the
// reader is the only one who knows how fast they can actually walk.
func (t Telegram) riskyNote(b domain.Brief, c domain.Candidate) string {
	return fmt.Sprintf("%s 若你 %s 前到站也搭得上（抵%s %s），但誤點可能縮短，不建議賭",
		esc(c.TrainNo), clock(c.EstDep.Add(-b.Params.BoardBuffer)),
		destSuffix(b.Route.DestinationName), clock(c.EstArr))
}

// ------------------------------------------------------------------ late (§9.2)

func (t Telegram) renderLate(b domain.Brief) string {
	var s strings.Builder
	rec := *b.Plan.Recommended

	title := "今日會遲到"
	if b.Mode == domain.ModeSevere {
		title = "今日嚴重延誤"
	}
	fmt.Fprintf(&s, "%s · %s\n\n", bold(title), t.header(b.GeneratedAt))

	// The baseline stays at the top even in this template: the value of a
	// compensation option is the difference from doing nothing, and that is
	// only legible if doing nothing is on the page.
	fmt.Fprintf(&s, "照常出門搭 %s\n", esc(rec.TrainNo))
	fmt.Fprintf(&s, "%s · %s %s → %s %s\n",
		delayPhrase(rec),
		esc(b.Route.OriginName), clock(rec.EstDep),
		esc(b.Route.DestinationName), clock(rec.EstArr))
	fmt.Fprintf(&s, "預計 %s 抵達 · %s\n",
		clock(rec.EstArr), bold(fmt.Sprintf("遲到 %d 分", rec.LatenessMinutes())))

	if comp := t.compensationBlock(b); comp != "" {
		s.WriteString("\n" + comp)
	}

	if cert := t.certificateBlock(b); cert != "" {
		s.WriteString("\n" + cert)
	}
	if warn := unknownTypeWarning(b.Plan); warn != "" {
		s.WriteString("\n" + warn + "\n")
	}

	fmt.Fprintf(&s, "\n%s\n", bold("全部班次"))
	s.WriteString(pre(t.candidateTable(b)))
	s.WriteString(t.footer(b))
	return s.String()
}

// compensationBlock reports an early-leave option only when one exists. A
// user who is already leaving as early as they can gains nothing from being
// told so; the block simply does not appear.
func (t Telegram) compensationBlock(b domain.Brief) string {
	best := b.BestCompensation()
	if best == nil {
		return ""
	}

	var s strings.Builder
	s.WriteString(bold("提早出門") + "\n")
	fmt.Fprintf(&s, "提早 %d 分（%s 到站）可搭 %s\n",
		best.EarlyLeaveMinutes(), clock(best.RequiredReady), esc(best.Candidate.TrainNo))
	fmt.Fprintf(&s, "%s · 抵%s %s\n",
		delayPhrase(best.Candidate), destSuffix(b.Route.DestinationName),
		clock(best.Candidate.EstArr))
	if best.Lateness == 0 {
		fmt.Fprintf(&s, "%s · 較照常出門少遲到 %d 分\n", bold("這樣做可以準時"), best.SavedMinutes())
	} else {
		fmt.Fprintf(&s, "%s · 較照常出門少遲到 %d 分\n",
			bold(fmt.Sprintf("仍遲到 %d 分", best.LatenessMinutes())), best.SavedMinutes())
	}
	return s.String()
}

func (t Telegram) certificateBlock(b domain.Brief) string {
	rec := b.Plan.Recommended
	if rec == nil || rec.Lateness == 0 {
		return ""
	}
	c := b.Certificate

	// The user is late and the railway is running fine: there is nothing to
	// certify, so the block does not appear.
	if !c.Found {
		return ""
	}

	var s strings.Builder
	s.WriteString(bold("誤點證明") + "\n")
	if c.Covered {
		fmt.Fprintf(&s, "抵達後可申請 %s 次（誤點 %d 分），足以涵蓋今日遲到\n",
			esc(c.TrainNo), c.DelayMinutes())
	} else {
		fmt.Fprintf(&s, "可申請 %s 次（誤點 %d 分），但遲到 %d 分，未必全數涵蓋\n",
			esc(c.TrainNo), c.DelayMinutes(), rec.LatenessMinutes())
	}
	if t.CertificateNote != "" {
		fmt.Fprintf(&s, "%s\n", esc(t.CertificateNote))
	}
	return s.String()
}

// ------------------------------------------------------- no service / degraded

func (t Telegram) renderNoService(b domain.Brief) string {
	var s strings.Builder
	fmt.Fprintf(&s, "%s · %s\n\n", bold("通勤簡報"), t.header(b.GeneratedAt))
	fmt.Fprintf(&s, "時間窗內查無可搭班次（%s → %s）\n",
		esc(b.Route.OriginName), esc(b.Route.DestinationName))
	if b.Plan.SuspendedCount > 0 {
		fmt.Fprintf(&s, "其中 %d 班停駛。\n", b.Plan.SuspendedCount)
	}
	s.WriteString("\n請自行以台鐵 App 確認。\n")
	s.WriteString(t.footer(b))
	return s.String()
}

// renderDegraded implements §9.3. Something is always sent: a missing message
// is indistinguishable from a quiet morning, and that ambiguity is the failure
// mode this system is built to avoid.
func (t Telegram) renderDegraded(b domain.Brief) string {
	var s strings.Builder
	fmt.Fprintf(&s, "%s\n\n", bold("通勤簡報產生失敗"))
	fmt.Fprintf(&s, "%s\n", esc(degradationText(b.Degradation)))

	if len(b.Plan.Candidates) > 0 {
		fmt.Fprintf(&s, "\n%s\n", bold("表定時刻（未套用誤點）"))
		var tb table
		tb.headers = []string{"NO.", "DEP", "ARR"}
		tb.aligns = []align{alignLeft, alignRight, alignRight}
		for _, c := range b.Plan.Candidates {
			tb.addRow(c.TrainNo, clock(c.SchedDep), clock(c.SchedArr))
		}
		s.WriteString(pre(tb.render()))
	}

	s.WriteString("\n請自行以台鐵 App 確認誤點狀況。\n")
	s.WriteString(t.footer(b))
	return s.String()
}

func degradationText(d *domain.Degradation) string {
	if d == nil {
		return "未知錯誤"
	}
	switch d.Stage {
	case "timetable":
		return "無法取得台鐵時刻表（" + d.Detail + "）"
	case "live":
		return "無法取得台鐵即時誤點資料（" + d.Detail + "），以下為表定時刻"
	default:
		return "執行失敗（" + d.Detail + "）"
	}
}

// ------------------------------------------------------------------- fragments

// candidateTable is the comparison grid both the normal and the late template
// print. They ask the same question of the same rows — which trains are
// there, how late, and what does that make of each one — so they share one
// layout rather than two that have to be kept looking alike.
func (t Telegram) candidateTable(b domain.Brief) string {
	var tb table
	// The status column carries no heading: the labels read as themselves,
	// and the key below the table translates them.
	tb.headers = []string{"NO.", "DLY", "DEP", "ARR", ""}
	tb.aligns = []align{alignLeft, alignRight, alignRight, alignRight, alignLeft}
	for _, c := range t.tableRows(b) {
		tb.addRow(c.TrainNo, delayCell(c), clock(c.EstDep), clock(c.EstArr), lateStatus(b, c))
	}
	return tb.render()
}

// tableRows takes the recommendation plus as many alternatives as configured,
// in the algorithm's own ranking order, and then makes sure the user's habitual
// trains appear even if the ranking pushed them past the cap.
//
// Their absence would itself be misread: seeing that 2008 has already gone is
// the fact the reader is looking for, whereas simply not finding 2008 in the
// table looks like a bug in the brief rather than a fact about the morning.
//
// Appending them preserves the global ranking, because Candidates is already
// ranked and the alternatives taken above are a prefix of it.
func (t Telegram) tableRows(b domain.Brief) []domain.Candidate {
	n := t.MaxAlternatives
	if n <= 0 {
		n = 4
	}

	rows := make([]domain.Candidate, 0, n+1)
	seen := map[string]bool{}
	if b.Plan.Recommended != nil {
		rows = append(rows, *b.Plan.Recommended)
		seen[b.Plan.Recommended.TrainNo] = true
	}
	for _, c := range b.Plan.TopAlternatives(n) {
		rows = append(rows, c)
		seen[c.TrainNo] = true
	}
	for _, c := range b.Plan.Candidates {
		if c.Usual && !seen[c.TrainNo] {
			rows = append(rows, c)
			seen[c.TrainNo] = true
		}
	}
	return rows
}

func status(b domain.Brief, c domain.Candidate) string {
	switch {
	case b.Plan.Recommended != nil && c.TrainNo == b.Plan.Recommended.TrainNo:
		return statusRecommended
	case c.Catchability == domain.Missed:
		return statusMissed
	case c.Catchability == domain.Risky:
		return statusRisky
	case c.Lateness > 0:
		return statusLate
	default:
		return statusCatchable
	}
}

// lateStatus is status, with the minutes late folded into the LATE label
// itself. This is the table's only record of the figure, now that there is no
// MIN column of its own.
func lateStatus(b domain.Brief, c domain.Candidate) string {
	s := status(b, c)
	if s == statusLate {
		return fmt.Sprintf("%s %dm", s, c.LatenessMinutes())
	}
	return s
}

// noDelayData marks a train the live board never mentioned. It reads as
// "unknown" rather than as "+0", so an assumption is never mistaken for a
// measurement; the note under the table spells that out in words.
const noDelayData = "--"

func delayCell(c domain.Candidate) string {
	if c.DelaySource == domain.DelaySourceNone {
		return noDelayData
	}
	return fmt.Sprintf("+%d", c.DelayMinutes())
}

func delayPhrase(c domain.Candidate) string {
	switch {
	case c.DelaySource == domain.DelaySourceNone:
		return "表定（無即時資料）"
	case c.Delay == 0:
		return "準點"
	default:
		return fmt.Sprintf("誤點 +%d", c.DelayMinutes())
	}
}

func unknownTypeWarning(p domain.Plan) string {
	if len(p.UnknownTypes) == 0 {
		return ""
	}
	return "注意：車種未知（" + esc(strings.Join(p.UnknownTypes, "、")) +
		"），請確認可否持電子票證乘車"
}

// footer always repeats the two hand-tuned parameters. They are the likeliest
// cause of a wrong recommendation, and printing them every day is what makes a
// drifting assumption noticeable before it becomes a habit.
func (t Telegram) footer(b domain.Brief) string {
	var s strings.Builder
	fmt.Fprintf(&s, "\n以 %s 抵站計算\n", clock(b.Params.Ready))
	if !b.DataUpdatedAt.IsZero() {
		fmt.Fprintf(&s, "資料更新 %s\n", b.DataUpdatedAt.Format("15:04:05"))
	}
	return s.String()
}

func (t Telegram) header(at time.Time) string {
	return fmt.Sprintf("%d/%d (%s) %s",
		int(at.Month()), at.Day(), weekdayZh[int(at.Weekday())], at.Format("15:04"))
}

// destSuffix abbreviates the destination for narrow table headers: 臺北 → 北.
func destSuffix(name string) string {
	r := []rune(name)
	if len(r) == 0 {
		return ""
	}
	return string(r[len(r)-1])
}

func clock(t time.Time) string { return t.Format("15:04") }

func minutes(d time.Duration) int { return int(d / time.Minute) }

func pre(s string) string { return "<pre>" + html.EscapeString(s) + "</pre>\n" }

func bold(s string) string { return "<b>" + html.EscapeString(s) + "</b>" }

func esc(s string) string { return html.EscapeString(s) }
