package domain

import (
	"fmt"
	"strings"
	"time"
)

// DateKey is a calendar date rendered as yyyy-MM-dd. Scheduling compares dates,
// not instants, and a string key keeps the state file readable by eye.
const dateLayout = "2006-01-02"

// DateKeyOf returns t's calendar date in its own location.
func DateKeyOf(t time.Time) string { return t.Format(dateLayout) }

// TimeOfDay is a wall-clock time with no date, as written in the config.
type TimeOfDay struct {
	Hour, Minute int
}

// ParseTimeOfDay accepts "HH:mm".
func ParseTimeOfDay(s string) (TimeOfDay, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return TimeOfDay{}, fmt.Errorf("invalid time of day %q, want HH:mm", s)
	}
	return TimeOfDay{Hour: t.Hour(), Minute: t.Minute()}, nil
}

func (t TimeOfDay) String() string { return fmt.Sprintf("%02d:%02d", t.Hour, t.Minute) }

// weekdayNames accepts the short and long English forms settings.json's
// notify_weekdays is written in (see internal/adapter/settingsfile), so the
// file stays human-readable.
var weekdayNames = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday,
	"mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday,
	"wed": time.Wednesday, "weds": time.Wednesday, "wednesday": time.Wednesday,
	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
	"fri": time.Friday, "friday": time.Friday,
	"sat": time.Saturday, "saturday": time.Saturday,
}

var weekdayShort = [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// ParseWeekday accepts a case-insensitive short or long English weekday name.
func ParseWeekday(s string) (time.Weekday, error) {
	if wd, ok := weekdayNames[strings.ToLower(strings.TrimSpace(s))]; ok {
		return wd, nil
	}
	return 0, fmt.Errorf("unknown weekday %q", s)
}

// ParseWeekdays splits a comma-separated list ("Mon,Tue,Wed,Thu,Fri") into
// weekdays, in the order given.
func ParseWeekdays(csv string) ([]time.Weekday, error) {
	var out []time.Weekday
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		wd, err := ParseWeekday(part)
		if err != nil {
			return nil, err
		}
		out = append(out, wd)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no weekdays given")
	}
	return out, nil
}

// WeekdayShort renders a weekday the way ParseWeekday reads it back, for
// /status and confirmation messages.
func WeekdayShort(w time.Weekday) string { return weekdayShort[w] }

// On resolves the time of day against a date, in that date's location.
func (t TimeOfDay) On(day time.Time) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), t.Hour, t.Minute, 0, 0, day.Location())
}

// Schedule is one rule from the config: a set of days plus a fire time. Days
// can be given as weekdays, as explicit dates (for make-up workdays), or both.
type Schedule struct {
	Name     string
	Weekdays []time.Weekday
	Dates    []string // yyyy-MM-dd
	At       TimeOfDay
}

// Matches reports whether the schedule applies on the given day.
func (s Schedule) Matches(day time.Time) bool {
	key := DateKeyOf(day)
	for _, d := range s.Dates {
		if d == key {
			return true
		}
	}
	wd := day.Weekday()
	for _, w := range s.Weekdays {
		if w == wd {
			return true
		}
	}
	return false
}

// Scheduling is the whole §10.3 rule set.
type Scheduling struct {
	Schedules []Schedule
	SkipDates []string // yyyy-MM-dd: leave, public holidays, business trips
	// Tolerance is how late a tick may be and still count as the scheduled
	// firing (tick_tolerance_minutes).
	Tolerance time.Duration
	// RetryWindow is how long a failed run keeps retrying on later ticks
	// before giving up and sending a degraded notice.
	RetryWindow time.Duration
}

// Skipped reports whether the given day is on the skip list.
func (s Scheduling) Skipped(day time.Time) bool {
	key := DateKeyOf(day)
	for _, d := range s.SkipDates {
		if d == key {
			return true
		}
	}
	return false
}

// Attempt records what happened to one schedule on one day.
type Attempt struct {
	Date   string    // yyyy-MM-dd of LastAt
	Count  int       // attempts made on that date
	LastAt time.Time // most recent attempt
	GaveUp bool      // the give-up notice for that date has been sent
}

// TickState is the persisted guard state, keyed by schedule name.
type TickState struct {
	// LastSuccess maps schedule name to the last date it succeeded on. This
	// is what makes the every-minute tick idempotent.
	LastSuccess map[string]string
	// Attempts maps schedule name to today's attempt record.
	Attempts map[string]Attempt
}

// SucceededOn reports whether the named schedule already delivered on day.
func (s TickState) SucceededOn(name string, day time.Time) bool {
	return s.LastSuccess[name] == DateKeyOf(day)
}

// AttemptOn returns the attempt record for the named schedule if it belongs to
// day, and the zero value otherwise — yesterday's counters must not gate today.
func (s TickState) AttemptOn(name string, day time.Time) Attempt {
	a := s.Attempts[name]
	if a.Date != DateKeyOf(day) {
		return Attempt{}
	}
	return a
}

// TickAction is what this minute's wake-up should do.
type TickAction int

const (
	// TickNone means the guard rejected this tick; the process exits quietly.
	// The overwhelming majority of the 1440 daily wake-ups end here, without
	// touching the network.
	TickNone TickAction = iota
	// TickRun means run the full brief.
	TickRun
	// TickGiveUp means the retry window expired without success; send the
	// degraded notice once so the day does not pass in silence.
	TickGiveUp
)

func (a TickAction) String() string {
	switch a {
	case TickRun:
		return "run"
	case TickGiveUp:
		return "give_up"
	default:
		return "none"
	}
}

// TickDecision is the outcome of the guard, including the reason, which goes
// straight into the structured log so a quiet morning can be explained.
type TickDecision struct {
	Action   TickAction
	Schedule Schedule
	// FiredAt is the schedule's nominal time today. T_ready derives from it
	// rather than from the current instant, so a tick that lands a minute
	// late still plans the same morning.
	FiredAt time.Time
	Reason  string
	// Retry is true when this run follows an earlier failed attempt today.
	Retry bool
}

// DecideTicks applies the §10.3 guard to every Schedule independently. It is
// a pure function of the current time, the configuration and the persisted
// state, which is the point: the entire scheduling behaviour is testable
// without a clock, a timer or a file.
//
// Every Schedule is evaluated on every call — nothing stops at the first
// match. Two Schedules due in the same minute (a plausible setup: someone
// running both an "上班通勤" and "下班通勤" rule) must both fire, not just
// whichever happens to be first in the list.
//
// When at least one Schedule is due (TickRun or TickGiveUp), the returned
// slice holds exactly those decisions — schedules that are simply not due yet
// are not reported, there being nothing actionable to say about them. When
// none are due, the slice holds exactly one TickNone decision whose Reason
// joins every Schedule's skip reason, so a quiet morning can still be
// explained from the log.
func DecideTicks(now time.Time, s Scheduling, st TickState) []TickDecision {
	if s.Skipped(now) {
		return []TickDecision{{Reason: "date on skip list"}}
	}

	var due []TickDecision
	// skipped records why each matching schedule declined to run, so a morning
	// that produced no message can be explained from the log rather than
	// guessed at. "No schedule due" and "already delivered" look identical
	// from the outside and have completely different causes.
	var skipped []string

	for _, sch := range s.Schedules {
		if !sch.Matches(now) {
			continue
		}
		firedAt := sch.At.On(now)
		if now.Before(firedAt) {
			skipped = append(skipped, sch.Name+": not yet due")
			continue
		}
		if st.SucceededOn(sch.Name, now) {
			skipped = append(skipped, sch.Name+": already delivered today")
			continue
		}

		attempt := st.AttemptOn(sch.Name, now)
		d := TickDecision{Schedule: sch, FiredAt: firedAt, Retry: attempt.Count > 0}

		if attempt.Count == 0 {
			// A first attempt is allowed only inside the tolerance window.
			// Outside it the schedule simply did not fire this morning — most
			// likely the host was down — and a brief delivered long after the
			// user has already left is worse than none.
			if !now.After(firedAt.Add(s.Tolerance)) {
				d.Action = TickRun
				d.Reason = "scheduled run"
				due = append(due, d)
				continue
			}
			skipped = append(skipped, sch.Name+": missed the tolerance window")
			continue
		}

		if !now.After(firedAt.Add(s.RetryWindow)) {
			d.Action = TickRun
			d.Reason = fmt.Sprintf("retry %d within window", attempt.Count+1)
			due = append(due, d)
			continue
		}
		if !attempt.GaveUp {
			d.Action = TickGiveUp
			d.Reason = fmt.Sprintf("retry window expired after %d attempts", attempt.Count)
			due = append(due, d)
			continue
		}
		skipped = append(skipped, sch.Name+": gave up earlier today")
	}

	if len(due) > 0 {
		return due
	}
	if len(skipped) > 0 {
		return []TickDecision{{Reason: strings.Join(skipped, "; ")}}
	}
	return []TickDecision{{Reason: "no schedule matches today"}}
}
