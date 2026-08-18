package render

import (
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

var testLoc = time.FixedZone("Asia/Taipei", 8*3600)

func at(hhmm string) time.Time {
	t, err := time.ParseInLocation("15:04", hhmm, testLoc)
	if err != nil {
		panic(err)
	}
	return time.Date(2026, 8, 18, t.Hour(), t.Minute(), 0, 0, testLoc)
}

func testParams() domain.Params {
	return domain.Params{
		Ready:       at("08:20"),
		Deadline:    at("09:30"),
		LastMile:    20 * time.Minute,
		BoardBuffer: 2 * time.Minute,
		RiskMargin:  3 * time.Minute,
	}
}

func svc(no, typeID, typeName, dep, arr string) domain.Service {
	return domain.Service{
		TrainNo: no, TypeID: typeID, TypeName: typeName,
		SchedDep: at(dep), SchedArr: at(arr),
	}
}

func usualServices() []domain.Service {
	return []domain.Service{
		svc("1136", "1131", "區間", "08:16", "08:57"),
		svc("2008", "1132", "區間快", "08:26", "09:02"),
		svc("1138", "1131", "區間", "08:34", "09:14"),
	}
}

func buildBrief(t *testing.T, services []domain.Service, delays map[string]int) domain.Brief {
	t.Helper()
	plan := domain.BuildPlan(domain.PlanInput{
		Services: services,
		Delays:   delays,
		Params:   testParams(),
		Window:   domain.Window{Lookback: 30 * time.Minute, Lookahead: 60 * time.Minute},
		Filter: domain.TypeFilter{
			ExcludedIDs:   map[string]bool{"1101": true, "1107": true},
			KnownKeywords: []string{"區間快", "區間", "自強", "莒光"},
			Policy:        domain.IncludeAndFlag,
		},
		UsualTrainNos: []string{"2008", "1136", "1138"},
	})
	return domain.BuildBrief(domain.BriefInput{
		GeneratedAt:         at("07:50"),
		Schedule:            "平日通勤",
		Route:               domain.Route{OriginName: "桃園", DestinationName: "臺北"},
		Params:              testParams(),
		Plan:                plan,
		LiveDataAvailable:   true,
		DataUpdatedAt:       at("07:49").Add(53 * time.Second),
		CertificateEnabled:  true,
		CertificateMinDelay: 5 * time.Minute,
		CompensationEnabled: true,
		MaxEarlyLeave:       15 * time.Minute,
		SevereThreshold:     30 * time.Minute,
	})
}

func testRenderer() Telegram {
	return Telegram{MaxAlternatives: 4, CertificateNote: "臺北站櫃檯人工申請"}
}

func TestRenderNormal(t *testing.T) {
	b := buildBrief(t, usualServices(), map[string]int{"1136": 0, "2008": 0, "1138": 0})
	msg := testRenderer().Render(b)

	if msg.ParseMode != "HTML" {
		t.Errorf("parse mode = %q, want HTML", msg.ParseMode)
	}
	for _, want := range []string{
		"<b>通勤簡報</b> · 8/18 (二) 07:50",
		"<b>建議搭乘 2008 區間快</b>",
		"桃園 08:26 → 臺北 09:02",
		"準點",
		"預計 09:22 打卡（餘裕 8 分）",
		"以 08:20 抵站、末端 20 分計算",
		"資料更新 07:49:53",
	} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("message is missing %q:\n%s", want, msg.Text)
		}
	}
	// A punctual morning uses the everyday template: no warning headline, no
	// compensation advice and no certificate block. The ⏰遲到 marker may
	// still appear against a slower alternative, which is a fact about that
	// train rather than about the user's morning.
	for _, unwanted := range []string{"今日會遲到", "今日嚴重延誤", "誤點證明", "提早出門"} {
		if strings.Contains(msg.Text, unwanted) {
			t.Errorf("a punctual brief should not contain %q:\n%s", unwanted, msg.Text)
		}
	}
}

// TestRenderShowsHabitualTrains checks the trains the user actually knows stay
// visible even once the ranking has demoted them. Their absence would read as
// a bug in the brief rather than a fact about the morning.
func TestRenderShowsHabitualTrains(t *testing.T) {
	b := buildBrief(t, usualServices(), map[string]int{"1136": 0, "2008": 0, "1138": 0})
	msg := testRenderer().Render(b)

	if !strings.Contains(msg.Text, "1136") {
		t.Errorf("the missed habitual train is not shown:\n%s", msg.Text)
	}
}

// TestRenderScheduledOnly checks a train with no live record is labelled 表定
// rather than +0, so an assumption is never read as an observation.
func TestRenderScheduledOnly(t *testing.T) {
	b := buildBrief(t, usualServices(), map[string]int{})
	msg := testRenderer().Render(b)

	if !strings.Contains(msg.Text, "表定") {
		t.Errorf("missing live data must be labelled 表定:\n%s", msg.Text)
	}
	if strings.Contains(msg.Text, "+0") {
		t.Errorf("an unobserved delay must not be printed as +0:\n%s", msg.Text)
	}
}

func TestRenderLate(t *testing.T) {
	b := buildBrief(t, usualServices(), map[string]int{"1136": 8, "2008": 24, "1138": 2})
	msg := testRenderer().Render(b)

	if b.Mode != domain.ModeLate {
		t.Fatalf("mode = %v, want late", b.Mode)
	}
	for _, want := range []string{
		"<b>今日會遲到</b>",
		"照常出門搭",
		"遲到",
		"<b>提早出門</b>",
		"<b>誤點證明</b>",
		"<b>全部班次</b>",
	} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("late brief is missing %q:\n%s", want, msg.Text)
		}
	}
	// The baseline must precede the compensation, so the value of getting up
	// early is legible as a difference.
	if strings.Index(msg.Text, "照常出門搭") > strings.Index(msg.Text, "<b>提早出門</b>") {
		t.Error("the do-nothing baseline should appear above the compensation option")
	}
}

func TestRenderSevere(t *testing.T) {
	b := buildBrief(t, usualServices(), map[string]int{"1136": 60, "2008": 60, "1138": 60})
	msg := testRenderer().Render(b)

	if !strings.Contains(msg.Text, "<b>今日嚴重延誤</b>") {
		t.Errorf("severe delays should escalate the title:\n%s", msg.Text)
	}
	// Whether a delay warrants contacting a manager is the reader's call, not
	// a suggestion the brief makes for them.
	if strings.Contains(msg.Text, "建議直接聯繫主管") {
		t.Errorf("the brief should not tell the reader what to do about the delay:\n%s", msg.Text)
	}
	// Suggesting is as far as it goes. Sending anything on the user's behalf
	// is beyond what this program is authorised to do.
	if strings.Contains(msg.Text, "已通知") || strings.Contains(msg.Text, "已寄出") {
		t.Errorf("the brief must never claim to have contacted anyone:\n%s", msg.Text)
	}
}

// TestRenderCertificateCovered checks the counter block names a train number,
// which is the entire practical point: at the window you must quote one.
func TestRenderCertificateCovered(t *testing.T) {
	services := []domain.Service{
		svc("3100", "1131", "區間", "07:50", "08:35"),
		svc("3101", "1131", "區間", "08:30", "09:05"),
	}
	b := buildBrief(t, services, map[string]int{"3100": 24, "3101": 6})
	msg := testRenderer().Render(b)

	if !strings.Contains(msg.Text, "可申請 3100 次") {
		t.Errorf("the certificate block should name train 3100:\n%s", msg.Text)
	}
	if !strings.Contains(msg.Text, "足以涵蓋今日遲到") {
		t.Errorf("a covering certificate should say so:\n%s", msg.Text)
	}
	if !strings.Contains(msg.Text, "臺北站櫃檯人工申請") {
		t.Errorf("the certificate note from the config should appear:\n%s", msg.Text)
	}
}

func TestRenderCertificateNotCovered(t *testing.T) {
	b := buildBrief(t, usualServices(), map[string]int{"1136": 6, "2008": 60, "1138": 30})
	msg := testRenderer().Render(b)

	if !strings.Contains(msg.Text, "未必全數涵蓋") {
		t.Errorf("a partial certificate must not overpromise:\n%s", msg.Text)
	}
}

// TestRenderCertificateUnavailable covers the case where the user is late on
// a punctual railway: there is nothing to certify, so the block does not
// appear at all rather than explaining its own absence.
func TestRenderCertificateUnavailable(t *testing.T) {
	p := testParams()
	p.LastMile = 45 * time.Minute
	plan := domain.BuildPlan(domain.PlanInput{
		Services: usualServices(),
		Delays:   map[string]int{"1136": 0, "2008": 0, "1138": 0},
		Params:   p,
		Window:   domain.Window{Lookback: 30 * time.Minute, Lookahead: 60 * time.Minute},
		Filter:   domain.TypeFilter{KnownKeywords: []string{"區間"}},
	})
	b := domain.BuildBrief(domain.BriefInput{
		GeneratedAt:         at("07:50"),
		Route:               domain.Route{OriginName: "桃園", DestinationName: "臺北"},
		Params:              p,
		Plan:                plan,
		LiveDataAvailable:   true,
		CertificateEnabled:  true,
		CertificateMinDelay: 5 * time.Minute,
		CompensationEnabled: true,
		MaxEarlyLeave:       15 * time.Minute,
		SevereThreshold:     30 * time.Minute,
	})

	msg := testRenderer().Render(b)
	if strings.Contains(msg.Text, "誤點證明") {
		t.Errorf("a brief with nothing to certify should omit the block entirely:\n%s", msg.Text)
	}
}

func TestRenderRiskyNote(t *testing.T) {
	b := buildBrief(t, usualServices(), map[string]int{"1136": 6, "2008": 6, "1138": 6})
	msg := testRenderer().Render(b)

	if !strings.Contains(msg.Text, "1136 若你") {
		t.Errorf("the risky train should be surfaced, not hidden:\n%s", msg.Text)
	}
	if !strings.Contains(msg.Text, "不建議賭") {
		t.Errorf("the risky note should say why it is not the recommendation:\n%s", msg.Text)
	}
	// The user boards two minutes before departure, so the note quotes the
	// time they must be there, not the departure time.
	if !strings.Contains(msg.Text, "08:20 前到站") {
		t.Errorf("the note should quote the arrival time needed:\n%s", msg.Text)
	}
}

func TestRenderUnknownTrainType(t *testing.T) {
	services := append(usualServices(), svc("777", "9999", "磁浮特快", "08:30", "09:00"))
	b := buildBrief(t, services, map[string]int{})
	msg := testRenderer().Render(b)

	if !strings.Contains(msg.Text, "車種未知") {
		t.Errorf("an unrecognised train type should be flagged:\n%s", msg.Text)
	}
	if !strings.Contains(msg.Text, "磁浮特快") {
		t.Errorf("the warning should name the type:\n%s", msg.Text)
	}
}

func TestRenderNoService(t *testing.T) {
	b := buildBrief(t, nil, map[string]int{})
	msg := testRenderer().Render(b)

	if !strings.Contains(msg.Text, "查無可搭班次") {
		t.Errorf("expected a no-service notice:\n%s", msg.Text)
	}
	if strings.TrimSpace(msg.Text) == "" {
		t.Error("even a no-service morning must produce a message")
	}
}

// TestRenderDegraded covers §9.3: a live-board failure still sends the
// scheduled times, clearly marked as not delay-adjusted.
func TestRenderDegraded(t *testing.T) {
	plan := domain.BuildPlan(domain.PlanInput{
		Services: usualServices(),
		Delays:   map[string]int{},
		Params:   testParams(),
		Window:   domain.Window{Lookback: 30 * time.Minute, Lookahead: 60 * time.Minute},
		Filter:   domain.TypeFilter{KnownKeywords: []string{"區間"}},
	})
	b := domain.DegradedBrief(domain.BriefInput{
		GeneratedAt: at("07:50"),
		Route:       domain.Route{OriginName: "桃園", DestinationName: "臺北"},
		Params:      testParams(),
		Plan:        plan,
	}, domain.Degradation{
		Stage:           "live",
		Detail:          "TDX API 逾時，已重試 3 次",
		SchedulesUsable: true,
	})

	msg := testRenderer().Render(b)
	for _, want := range []string{
		"<b>通勤簡報產生失敗</b>",
		"TDX API 逾時，已重試 3 次",
		"表定時刻（未套用誤點）",
		"2008",
		"請自行以台鐵 App 確認",
	} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("degraded brief is missing %q:\n%s", want, msg.Text)
		}
	}
}

func TestRenderDegradedWithoutTimetable(t *testing.T) {
	b := domain.DegradedBrief(domain.BriefInput{
		GeneratedAt: at("07:50"),
		Route:       domain.Route{OriginName: "桃園", DestinationName: "臺北"},
		Params:      testParams(),
	}, domain.Degradation{Stage: "timetable", Detail: "HTTP 500"})

	msg := testRenderer().Render(b)
	if !strings.Contains(msg.Text, "無法取得台鐵時刻表") {
		t.Errorf("expected the timetable failure to be named:\n%s", msg.Text)
	}
	if strings.TrimSpace(msg.Text) == "" {
		t.Error("a total failure must still produce a message")
	}
}

// TestHTMLEscaping checks hostile or merely awkward API content cannot break
// the markup. Telegram rejects the whole message on a parse error, so an
// unescaped angle bracket would turn a bad morning into a silent one.
func TestHTMLEscaping(t *testing.T) {
	services := []domain.Service{svc("<b>x</b>", "1131", "區間 & <i>快</i>", "08:30", "09:00")}
	b := buildBrief(t, services, map[string]int{})
	msg := testRenderer().Render(b)

	if strings.Contains(msg.Text, "<b>x</b>") {
		t.Errorf("train numbers must be escaped:\n%s", msg.Text)
	}
	if strings.Contains(msg.Text, "<i>快</i>") {
		t.Errorf("type names must be escaped:\n%s", msg.Text)
	}
	// The only tags left standing should be the ones the layout introduces
	// itself: <pre> for the table and <b> for headings.
	allowed := map[string]bool{"<pre>": true, "</pre>": true, "<b>": true, "</b>": true}
	for _, tag := range regexp.MustCompile(`</?[a-zA-Z][^>]*>`).FindAllString(msg.Text, -1) {
		if !allowed[tag] {
			t.Errorf("unexpected tag %q in:\n%s", tag, msg.Text)
		}
	}
}

// maxTableWidth is the character budget for a <pre> block.
//
// Telegram's mobile clients wrap a preformatted block at roughly forty
// monospace characters, and a wrap there destroys the column alignment that is
// the only reason for using a monospace block at all. Thirty-six leaves margin
// for the narrowest phone in portrait; an ordinary morning comes in well under
// it, and only a three-digit delay approaches it.
const maxTableWidth = 36

// TestPreBlocksAreASCII enforces the invariant the whole layout rests on.
//
// Telegram's monospace font does not render a Chinese glyph at exactly twice
// the width of a Latin one, so any CJK inside an aligned grid comes out
// crooked on the handset no matter how the widths are computed. Chinese is
// therefore confined to the prose around the table.
func TestPreBlocksAreASCII(t *testing.T) {
	for name, b := range widthTestBriefs(t) {
		for _, block := range preBlocks(testRenderer().Render(b).Text) {
			for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
				if !isASCII(line) {
					t.Errorf("%s: non-ASCII inside a <pre> block, which will not align:\n%q",
						name, line)
				}
			}
		}
	}
}

// TestPreLinesDoNotStartWithSpace guards a rendering detail that silently
// broke the table on a real handset: Telegram strips leading whitespace from a
// line even inside a <pre> block. A layout whose first column could be blank
// therefore lost its indent on exactly those rows, so they slid left while
// every other row stayed aligned.
//
// The structural fix is that the first column is always the train number, and
// this test is what keeps it that way.
func TestPreLinesDoNotStartWithSpace(t *testing.T) {
	for name, b := range widthTestBriefs(t) {
		for _, block := range preBlocks(testRenderer().Render(b).Text) {
			for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
				if strings.HasPrefix(line, " ") {
					t.Errorf("%s: line starts with a space and will lose its indent:\n%q",
						name, line)
				}
			}
		}
	}
}

// TestTableFitsPhoneWidth protects the format's reason for existing. A ruled
// table of these columns measured forty characters and wrapped on a real
// handset, which is what drove the layout to drop its rules.
func TestTableFitsPhoneWidth(t *testing.T) {
	for name, b := range widthTestBriefs(t) {
		for _, block := range preBlocks(testRenderer().Render(b).Text) {
			for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
				if got := cellWidth(line); got > maxTableWidth {
					t.Errorf("%s: line is %d characters, over the %d budget:\n%s",
						name, got, maxTableWidth, line)
				}
			}
		}
	}
}

func widthTestBriefs(t *testing.T) map[string]domain.Brief {
	t.Helper()
	return map[string]domain.Brief{
		"on time":  buildBrief(t, usualServices(), map[string]int{"1136": 0, "2008": 0, "1138": 0}),
		"late":     buildBrief(t, usualServices(), map[string]int{"1136": 8, "2008": 24, "1138": 2}),
		"severe":   buildBrief(t, usualServices(), map[string]int{"1136": 90, "2008": 120, "1138": 90}),
		"no live":  buildBrief(t, usualServices(), map[string]int{}),
		"degraded": degradedTestBrief(t),
	}
}

func degradedTestBrief(t *testing.T) domain.Brief {
	t.Helper()
	plan := domain.BuildPlan(domain.PlanInput{
		Services: usualServices(),
		Delays:   map[string]int{},
		Params:   testParams(),
		Window:   domain.Window{Lookback: 30 * time.Minute, Lookahead: 60 * time.Minute},
		Filter:   domain.TypeFilter{KnownKeywords: []string{"區間"}},
	})
	return domain.DegradedBrief(domain.BriefInput{
		GeneratedAt: at("07:50"),
		Route:       domain.Route{OriginName: "桃園", DestinationName: "臺北"},
		Params:      testParams(),
		Plan:        plan,
	}, domain.Degradation{Stage: "live", Detail: "TDX timeout", SchedulesUsable: true})
}

// TestTableLayout pins the exact grid. With no rules, the padding is the only
// thing holding the columns together, so it is worth asserting literally.
func TestTableLayout(t *testing.T) {
	var tb table
	tb.headers = []string{"NO.", "DLY", "DEP", "ARR", ""}
	tb.aligns = []align{alignLeft, alignRight, alignRight, alignRight, alignLeft}
	tb.addRow("1138", "+2", "08:36", "09:16", "REC")
	tb.addRow("2008", "+24", "08:50", "09:26", "LATE")
	tb.addRow("278", "--", "08:55", "09:27", "OK")

	want := "" +
		"NO.   DLY    DEP    ARR\n" +
		"1138   +2  08:36  09:16  REC\n" +
		"2008  +24  08:50  09:26  LATE\n" +
		"278    --  08:55  09:27  OK\n"

	if got := tb.render(); got != want {
		t.Errorf("table layout:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// TestTableWidensForContent checks the columns are measured, not fixed, so an
// unusually long value cannot push the grid out of true.
func TestTableWidensForContent(t *testing.T) {
	var tb table
	tb.headers = []string{"NO.", "DLY"}
	tb.aligns = []align{alignLeft, alignRight}
	tb.addRow("12345", "+120")

	want := "" +
		"NO.     DLY\n" +
		"12345  +120\n"

	if got := tb.render(); got != want {
		t.Errorf("table layout:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// TestTableHasNoRules pins the decision that the rules are gone. Reinstating
// them would silently push the table back over the wrap threshold.
func TestTableHasNoRules(t *testing.T) {
	b := buildBrief(t, usualServices(), map[string]int{"1136": 8, "2008": 24, "1138": 2})
	msg := testRenderer().Render(b)

	for _, r := range "┌┬┐├┼┤└┴┘│─" {
		if strings.ContainsRune(msg.Text, r) {
			t.Errorf("box-drawing character %q is back; it costs the width budget", r)
		}
	}
}

// TestNoEmoji checks the message stays free of pictographs. They were dropped
// deliberately: bold headings and plain symbols carry the same structure, and
// inside the table an emoji would also break the ASCII width assumption.
func TestNoEmoji(t *testing.T) {
	for name, b := range widthTestBriefs(t) {
		for _, r := range testRenderer().Render(b).Text {
			if isPictograph(r) {
				t.Errorf("%s: message contains the emoji %q", name, string(r))
			}
		}
	}
}

func isPictograph(r rune) bool {
	switch {
	case r >= 0x1F300 && r <= 0x1FAFF, // symbols, pictographs, extensions
		r >= 0x2600 && r <= 0x27BF, // misc symbols and dingbats
		r >= 0x1F000 && r <= 0x1F0FF,
		r == 0xFE0F, r == 0x20E3:
		return true
	}
	return false
}

// TestLatenessTableMinutes checks a late alternative's minutes are folded
// into its LATE label, since the table has no separate minutes column.
func TestLatenessTableMinutes(t *testing.T) {
	b := buildBrief(t, usualServices(), map[string]int{"1136": 8, "2008": 24, "1138": 2})
	msg := testRenderer().Render(b)

	if !strings.Contains(msg.Text, "LATE") || !strings.Contains(msg.Text, "m") {
		t.Errorf("a late alternative should show its minutes inline:\n%s", msg.Text)
	}
	if strings.Contains(msg.Text, "MIN") {
		t.Errorf("the lateness table has no MIN column:\n%s", msg.Text)
	}
}

// TestNoLegendUnderTable checks no template prints a key translating the
// table's abbreviations: the columns and status labels are left to speak
// for themselves.
func TestNoLegendUnderTable(t *testing.T) {
	for name, b := range widthTestBriefs(t) {
		msg := testRenderer().Render(b)
		if strings.Contains(msg.Text, "DLY 誤點") {
			t.Errorf("%s: should not print a column key:\n%s", name, msg.Text)
		}
	}
}

// preBlocks extracts and unescapes the <pre> sections, which is where every
// alignment-sensitive layout lives.
func preBlocks(text string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`(?s)<pre>(.*?)</pre>`).FindAllStringSubmatch(text, -1) {
		out = append(out, html.UnescapeString(m[1]))
	}
	return out
}

func TestCellWidth(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"2008", 4},
		{"08:26", 5},
		{"> ", 2},
		{"--", 2},
	}
	for _, tc := range tests {
		if got := cellWidth(tc.in); got != tc.want {
			t.Errorf("cellWidth(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestIsASCII(t *testing.T) {
	for _, s := range []string{"NO.  DLY  DEP", "> 2008  +24", "--", ""} {
		if !isASCII(s) {
			t.Errorf("isASCII(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"車次", "abc車", "→", "✅", "\t"} {
		if isASCII(s) {
			t.Errorf("isASCII(%q) = true, want false", s)
		}
	}
}

func TestPadding(t *testing.T) {
	if got := padRight("2008", 8); cellWidth(got) != 8 {
		t.Errorf("padRight width = %d, want 8", cellWidth(got))
	}
	if got := padLeft("+24", 6); !strings.HasSuffix(got, "+24") {
		t.Errorf("padLeft(%q) should right-align", got)
	}

	// Content wider than the column is never truncated: a clipped train
	// number is worse than a crooked table.
	if got := padRight("verylongvalue", 4); got != "verylongvalue" {
		t.Errorf("padRight truncated to %q", got)
	}
}

func TestWeekdayHeader(t *testing.T) {
	// 2026-08-18 is a Tuesday.
	if got := testRenderer().header(at("07:50")); got != "8/18 (二) 07:50" {
		t.Errorf("header = %q, want \"8/18 (二) 07:50\"", got)
	}
}

func TestDestSuffix(t *testing.T) {
	tests := map[string]string{"臺北": "北", "桃園": "園", "": "", "南港": "港"}
	for in, want := range tests {
		if got := destSuffix(in); got != want {
			t.Errorf("destSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}
