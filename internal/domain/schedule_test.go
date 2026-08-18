package domain

import (
	"strings"
	"testing"
	"time"
)

func testScheduling() Scheduling {
	return Scheduling{
		Schedules: []Schedule{
			{
				Name: "平日通勤",
				Weekdays: []time.Weekday{
					time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday,
				},
				At: TimeOfDay{Hour: 7, Minute: 50},
			},
			{
				Name:  "補班日",
				Dates: []string{"2026-09-26"},
				At:    TimeOfDay{Hour: 7, Minute: 50},
			},
		},
		Tolerance:   2 * time.Minute,
		RetryWindow: 10 * time.Minute,
	}
}

// on builds an instant on an arbitrary date, for the scheduling tests that need
// to move around the calendar.
func on(date, hhmm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", date+" "+hhmm, testLoc)
	if err != nil {
		panic(err)
	}
	return t
}

func TestTickWeekdayHit(t *testing.T) {
	// 2026-08-19 is a Wednesday.
	d := DecideTick(on("2026-08-19", "07:50"), testScheduling(), TickState{})

	if d.Action != TickRun {
		t.Fatalf("action = %v (%s), want run", d.Action, d.Reason)
	}
	if d.Schedule.Name != "平日通勤" {
		t.Errorf("schedule = %s, want 平日通勤", d.Schedule.Name)
	}
	// T_ready derives from the nominal time, not from the tick, so a tick
	// that lands a minute late still plans the same morning.
	if !d.FiredAt.Equal(on("2026-08-19", "07:50")) {
		t.Errorf("fired at = %s, want 07:50", d.FiredAt.Format("15:04"))
	}
}

func TestTickWeekendMiss(t *testing.T) {
	// 2026-08-22 is a Saturday, and no weekday rule covers it.
	if d := DecideTick(on("2026-08-22", "07:50"), testScheduling(), TickState{}); d.Action != TickNone {
		t.Errorf("action = %v, want none on a Saturday", d.Action)
	}
}

// TestTickMakeUpWorkday covers the make-up workdays that Taiwan schedules onto
// Saturdays. They are listed by hand rather than derived from a government
// calendar feed.
func TestTickMakeUpWorkday(t *testing.T) {
	// 2026-09-26 is a Saturday and is on the make-up list.
	d := DecideTick(on("2026-09-26", "07:50"), testScheduling(), TickState{})

	if d.Action != TickRun {
		t.Fatalf("action = %v (%s), want run on the make-up workday", d.Action, d.Reason)
	}
	if d.Schedule.Name != "補班日" {
		t.Errorf("schedule = %s, want 補班日", d.Schedule.Name)
	}
}

func TestTickSkipDate(t *testing.T) {
	rules := testScheduling()
	rules.SkipDates = []string{"2026-08-19"}

	d := DecideTick(on("2026-08-19", "07:50"), rules, TickState{})
	if d.Action != TickNone {
		t.Errorf("action = %v, want none on a skipped date", d.Action)
	}
}

// TestTickIdempotent is what makes a once-a-minute timer safe: having already
// delivered today, every remaining tick of the day must do nothing.
func TestTickIdempotent(t *testing.T) {
	state := TickState{LastSuccess: map[string]string{"平日通勤": "2026-08-19"}}

	for _, hhmm := range []string{"07:51", "07:52", "08:30", "23:59"} {
		if d := DecideTick(on("2026-08-19", hhmm), testScheduling(), state); d.Action != TickNone {
			t.Errorf("at %s: action = %v, want none after a successful delivery", hhmm, d.Action)
		}
	}
	// Tomorrow must still fire: yesterday's success is not today's.
	if d := DecideTick(on("2026-08-20", "07:50"), testScheduling(), state); d.Action != TickRun {
		t.Errorf("next day: action = %v, want run", d.Action)
	}
}

// TestTickTolerance checks a late first tick is still accepted, but only
// briefly: a brief delivered long after the user has left is worse than none.
func TestTickTolerance(t *testing.T) {
	tests := []struct {
		hhmm string
		want TickAction
	}{
		{"07:49", TickNone}, // before the scheduled time
		{"07:50", TickRun},
		{"07:52", TickRun},  // exactly the tolerance edge
		{"07:53", TickNone}, // past it, with no attempt on record
	}
	for _, tc := range tests {
		got := DecideTick(on("2026-08-19", tc.hhmm), testScheduling(), TickState{})
		if got.Action != tc.want {
			t.Errorf("at %s: action = %v, want %v (%s)", tc.hhmm, got.Action, tc.want, got.Reason)
		}
	}
}

// TestTickRetryWindow covers the self-healing property of the design: a run
// that fails at 07:50 is retried on later ticks, but not forever — otherwise a
// day-long TDX outage would deliver 1440 failure messages.
func TestTickRetryWindow(t *testing.T) {
	failed := TickState{Attempts: map[string]Attempt{
		"平日通勤": {Date: "2026-08-19", Count: 1, LastAt: on("2026-08-19", "07:50")},
	}}

	// Inside the window, keep retrying.
	for _, hhmm := range []string{"07:51", "07:55", "08:00"} {
		d := DecideTick(on("2026-08-19", hhmm), testScheduling(), failed)
		if d.Action != TickRun {
			t.Errorf("at %s: action = %v, want a retry (%s)", hhmm, d.Action, d.Reason)
		}
		if !d.Retry {
			t.Errorf("at %s: retry flag not set", hhmm)
		}
	}

	// Past it, give up exactly once, so the day does not end in silence.
	d := DecideTick(on("2026-08-19", "08:01"), testScheduling(), failed)
	if d.Action != TickGiveUp {
		t.Fatalf("action = %v, want give up past the retry window (%s)", d.Action, d.Reason)
	}

	// Once the give-up notice has gone out, stay quiet.
	failed.Attempts["平日通勤"] = Attempt{
		Date: "2026-08-19", Count: 1, LastAt: on("2026-08-19", "07:50"), GaveUp: true,
	}
	if d := DecideTick(on("2026-08-19", "08:02"), testScheduling(), failed); d.Action != TickNone {
		t.Errorf("action = %v, want none after the give-up notice was sent", d.Action)
	}
}

// TestTickStaleAttemptIgnored checks yesterday's counters cannot gate today.
func TestTickStaleAttemptIgnored(t *testing.T) {
	stale := TickState{Attempts: map[string]Attempt{
		"平日通勤": {Date: "2026-08-18", Count: 9, LastAt: on("2026-08-18", "07:50"), GaveUp: true},
	}}

	d := DecideTick(on("2026-08-19", "07:50"), testScheduling(), stale)
	if d.Action != TickRun {
		t.Fatalf("action = %v, want run: yesterday's failures must not gate today", d.Action)
	}
	if d.Retry {
		t.Error("today's first attempt should not be marked as a retry")
	}
}

func TestScheduleMatches(t *testing.T) {
	s := Schedule{
		Name:     "test",
		Weekdays: []time.Weekday{time.Monday},
		Dates:    []string{"2026-12-25"},
	}
	tests := []struct {
		date string
		want bool
	}{
		{"2026-08-17", true},  // a Monday
		{"2026-08-18", false}, // a Tuesday
		{"2026-12-25", true},  // an explicit date, a Friday
	}
	for _, tc := range tests {
		if got := s.Matches(on(tc.date, "07:50")); got != tc.want {
			t.Errorf("Matches(%s) = %v, want %v", tc.date, got, tc.want)
		}
	}
}

func TestParseTimeOfDay(t *testing.T) {
	got, err := ParseTimeOfDay(" 07:50 ")
	if err != nil {
		t.Fatalf("ParseTimeOfDay: %v", err)
	}
	if got.Hour != 7 || got.Minute != 50 {
		t.Errorf("got %v, want 07:50", got)
	}
	for _, bad := range []string{"", "7:5:0", "25:00", "seven fifty"} {
		if _, err := ParseTimeOfDay(bad); err == nil {
			t.Errorf("ParseTimeOfDay(%q) should have failed", bad)
		}
	}
}

// TestTickReasonsAreSpecific checks the log can answer "why was there no
// message this morning?". An unhelpful reason turns a five second diagnosis
// into an afternoon of guessing.
func TestTickReasonsAreSpecific(t *testing.T) {
	tests := []struct {
		name  string
		when  time.Time
		state TickState
		want  string
	}{
		{
			name: "no rule covers today",
			when: on("2026-08-22", "07:50"), // a Saturday
			want: "no schedule matches today",
		},
		{
			name: "before the scheduled time",
			when: on("2026-08-19", "06:00"),
			want: "not yet due",
		},
		{
			name:  "already delivered",
			when:  on("2026-08-19", "09:00"),
			state: TickState{LastSuccess: map[string]string{"平日通勤": "2026-08-19"}},
			want:  "already delivered today",
		},
		{
			name: "host was down through the window",
			when: on("2026-08-19", "11:00"),
			want: "missed the tolerance window",
		},
		{
			name: "gave up earlier",
			when: on("2026-08-19", "11:00"),
			state: TickState{Attempts: map[string]Attempt{
				"平日通勤": {Date: "2026-08-19", Count: 3, LastAt: on("2026-08-19", "07:55"), GaveUp: true},
			}},
			want: "gave up earlier today",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := DecideTick(tc.when, testScheduling(), tc.state)
			if d.Action != TickNone {
				t.Fatalf("action = %v, want none", d.Action)
			}
			if !strings.Contains(d.Reason, tc.want) {
				t.Errorf("reason = %q, should mention %q", d.Reason, tc.want)
			}
		})
	}
}
