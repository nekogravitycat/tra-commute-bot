// Package command implements the Telegram command interface of §10: /setup,
// /manage, /status, /help and /cancel, plus the four shared sub-flows
// (§10.4) they are both built from.
//
// A Schedule has exactly two states — absent or fully populated (§10.2
// invariant 1) — so the whole package is organised around collecting an
// ordered list of Field values into a domain.Settings draft, one question at
// a time, and only ever writing the draft back to the settings actor once
// every field in that list has an answer. /setup collects all eight fields
// before its first write; /manage's field-level edits collect just the one
// or two fields a menu item covers, but go through the exact same collection
// loop — see flows.go.
package command

import (
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// Field is one question in a collection flow.
type Field int

// The fields collected by a schedule flow, in collection order.
const (
	FieldName Field = iota
	FieldOrigin
	FieldDestination
	FieldReadyAt
	FieldDeadlineAt
	FieldWeekdays
	FieldNotifyAt
	FieldMaxEarlyLeave
)

// setupFields is the full question order for /setup (§10.5).
var setupFields = []Field{
	FieldName, FieldOrigin, FieldDestination,
	FieldReadyAt, FieldDeadlineAt,
	FieldWeekdays, FieldNotifyAt, FieldMaxEarlyLeave,
}

// Session is one chat's in-progress flow: either /setup building a brand new
// Schedule, or /manage editing (a subset of the fields of) an existing one.
// It also stands in for /manage's "resting" states — viewing the schedule
// list or one schedule's card — between flows, so a stray text message
// outside any flow has something to say "no" from.
//
// Sessions live in memory only. A process restart mid-flow loses it, which
// is an acceptable simplification per the timeout behaviour below: the user
// just sees the same "nothing in progress" state a 10-minute idle timeout
// would have produced anyway, and /setup or /manage starts them over.
type Session struct {
	ChatID int64

	// Editing is "" while building a brand new Schedule via /setup, and the
	// Settings.Name being edited while inside a /manage field edit or while
	// simply viewing that Schedule's card. It is what tells finishFlow
	// whether completing the field list means "show the create-confirmation
	// card" (§10.5) or "apply the edit immediately" (§10.6).
	Editing string
	// Original is a snapshot of the Schedule as it was before this flow
	// began, used both to validate a rename (Upsert needs the old name to
	// remove) and to render the "X 已從 A 改為 B" diff (§10.7 point 5).
	Original domain.Settings
	// Draft accumulates answers as they come in. For /setup it starts at the
	// zero value; for a /manage edit it starts as a copy of Original, so a
	// field the flow never touches (e.g. editing just T_ready leaves Route
	// alone) still has its real value when the draft is written back.
	Draft domain.Settings

	// Fields and Cursor track collection progress. Fields is nil while a
	// Session merely represents a resting /manage view (the schedule list or
	// one schedule's card) rather than an active question. Cursor equal to
	// len(Fields) means every field has an answer and the flow is ready to
	// finish (see finishFlow in flows.go).
	Fields []Field
	Cursor int

	// StationMatches holds subflow A's pending disambiguation list (§10.4-A)
	// between sending the inline keyboard and the callback that picks one.
	StationMatches []domain.Station
	// PickerSelected holds subflow C's in-progress weekday multi-select
	// (§10.4-C).
	PickerSelected map[time.Weekday]bool
	// PickerMessageID is the weekday keyboard's message ID, so toggling a day
	// edits the existing message in place instead of resending it.
	PickerMessageID int

	// AwaitingUsualTrainNo is /usualtrain's one piece of session state
	// (§usualtrain.go): true after "➕ 新增常搭班次" is pressed, until the
	// next text message answers it. It sits outside the Field/Draft
	// machinery above because a habitual-train list belongs to the whole
	// SettingsList, not to one Schedule being built or edited.
	AwaitingUsualTrainNo bool

	UpdatedAt time.Time
}

// sessionTimeout is the "idle for a while" cutoff after which a stale
// session is dropped instead of resumed (§10.5's "輸入逾時視同 /cancel").
// There is no proactive timer — see the package doc — so this is checked
// lazily, against whatever update arrives next.
const sessionTimeout = 10 * time.Minute

func (s *Session) stale(now time.Time) bool {
	return s == nil || now.Sub(s.UpdatedAt) > sessionTimeout
}

// currentField returns the field currently being asked, and whether there is
// one at all (false once every field in Fields has been answered, or if
// Fields is nil because the session merely represents a resting view).
func (s *Session) currentField() (Field, bool) {
	if s.Cursor < 0 || s.Cursor >= len(s.Fields) {
		return 0, false
	}
	return s.Fields[s.Cursor], true
}

func newSetupSession(chatID int64, now time.Time) *Session {
	return &Session{ChatID: chatID, Fields: append([]Field{}, setupFields...), UpdatedAt: now}
}

func newEditSession(chatID int64, existing domain.Settings, fields []Field, now time.Time) *Session {
	return &Session{
		ChatID:    chatID,
		Editing:   existing.Name,
		Original:  existing,
		Draft:     existing,
		Fields:    fields,
		UpdatedAt: now,
	}
}
