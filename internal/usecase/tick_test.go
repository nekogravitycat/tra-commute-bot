package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
	"github.com/nekogravitycat/tra-commute-bot/internal/platform/clock"
)

func newTestTick(now time.Time, st *fakeState, b *Brief) *Tick {
	return newTestTickWithSettings(now, st, &fakeSettings{}, b)
}

func newTestTickWithSettings(now time.Time, st *fakeState, settings *fakeSettings, b *Brief) *Tick {
	return &Tick{
		Clock:       clock.Fixed{At: now},
		State:       st,
		Settings:    settings,
		Brief:       b,
		Log:         quietLogger(),
		Tolerance:   2 * time.Minute,
		RetryWindow: 10 * time.Minute,
	}
}

// TestTickRunsAndRecords covers the whole loop: the guard fires, the brief goes
// out, and the state file remembers so the remaining ticks stay quiet.
func TestTickRunsAndRecords(t *testing.T) {
	n := &fakeNotifier{}
	b := newTestBrief(&fakeTimetable{services: usualServices()},
		&fakeDelays{delays: map[string]int{}}, &fakeRenderer{}, n)
	st := &fakeState{}

	// 2026-08-18 is a Tuesday.
	res, err := newTestTick(at("07:50"), st, b).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !res.Ran {
		t.Fatalf("the tick did not run: %s", res.Decision.Reason)
	}
	if len(n.sent) != 1 {
		t.Errorf("sent %d messages, want 1", len(n.sent))
	}
	if got := st.state.LastSuccess["commute"]; got != "2026-08-18" {
		t.Errorf("recorded success = %q, want 2026-08-18", got)
	}
}

// TestTickIdempotent is the property that makes an every-minute timer safe.
func TestTickIdempotent(t *testing.T) {
	n := &fakeNotifier{}
	b := newTestBrief(&fakeTimetable{services: usualServices()},
		&fakeDelays{delays: map[string]int{}}, &fakeRenderer{}, n)
	st := &fakeState{}

	if _, err := newTestTick(at("07:50"), st, b).Run(context.Background()); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	for _, hhmm := range []string{"07:51", "07:52", "08:15"} {
		res, err := newTestTick(at(hhmm), st, b).Run(context.Background())
		if err != nil {
			t.Fatalf("tick at %s: %v", hhmm, err)
		}
		if res.Ran {
			t.Errorf("the tick at %s ran again after a successful delivery", hhmm)
		}
	}
	if len(n.sent) != 1 {
		t.Errorf("sent %d messages, want exactly 1 for the day", len(n.sent))
	}
}

// TestTickQuietWhenNotDue checks the common case costs nothing: the guard
// rejects the tick before any adapter is touched.
func TestTickQuietWhenNotDue(t *testing.T) {
	tt := &fakeTimetable{services: usualServices()}
	d := &fakeDelays{delays: map[string]int{}}
	n := &fakeNotifier{}
	b := newTestBrief(tt, d, &fakeRenderer{}, n)

	res, err := newTestTick(at("06:30"), &fakeState{}, b).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Ran {
		t.Error("the tick ran outside its schedule")
	}
	if tt.calls != 0 || d.calls != 0 || len(n.sent) != 0 {
		t.Errorf("an idle tick touched the network: %d/%d calls, %d messages",
			tt.calls, d.calls, len(n.sent))
	}
}

// TestTickRecordsAttemptBeforeRunning checks the attempt is written first. If
// the process dies mid-flight, the next tick must still see that an attempt
// happened, or the retry window never starts counting.
func TestTickRecordsAttemptBeforeRunning(t *testing.T) {
	n := &fakeNotifier{failFor: 99}
	b := newTestBrief(&fakeTimetable{services: usualServices()},
		&fakeDelays{delays: map[string]int{}}, &fakeRenderer{}, n)
	st := &fakeState{}

	if _, err := newTestTick(at("07:50"), st, b).Run(context.Background()); err == nil {
		t.Fatal("expected the failed delivery to be reported")
	}

	a := st.state.Attempts["commute"]
	if a.Count != 1 || a.Date != "2026-08-18" {
		t.Errorf("attempt = %+v, want one attempt recorded for today", a)
	}
	if st.state.LastSuccess["commute"] != "" {
		t.Error("a failed delivery must not be recorded as a success")
	}
}

// TestTickRetriesThenGivesUp covers the self-healing window and its end: a day
// long outage must not deliver 1440 failure messages.
func TestTickRetriesThenGivesUp(t *testing.T) {
	n := &fakeNotifier{failFor: 99}
	b := newTestBrief(&fakeTimetable{services: usualServices()},
		&fakeDelays{delays: map[string]int{}}, &fakeRenderer{}, n)
	st := &fakeState{}

	// Every tick inside the window retries.
	for _, hhmm := range []string{"07:50", "07:51", "07:55"} {
		res, _ := newTestTick(at(hhmm), st, b).Run(context.Background())
		if !res.Ran {
			t.Errorf("the tick at %s did not retry: %s", hhmm, res.Decision.Reason)
		}
	}
	if got := st.state.Attempts["commute"].Count; got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}

	// Past the window, give up once.
	n.failFor = 0 // the give-up notice itself can get through
	res, err := newTestTick(at("08:05"), st, b).Run(context.Background())
	if err != nil {
		t.Fatalf("give-up tick: %v", err)
	}
	if res.Decision.Action != domain.TickGiveUp {
		t.Fatalf("action = %v, want give up", res.Decision.Action)
	}
	if res.Result.Brief.Mode != domain.ModeDegraded {
		t.Errorf("mode = %v, want degraded", res.Result.Brief.Mode)
	}
	if !st.state.Attempts["commute"].GaveUp {
		t.Error("the give-up must be recorded so it is not repeated")
	}

	// And stay quiet afterwards.
	before := len(n.sent)
	res, _ = newTestTick(at("08:06"), st, b).Run(context.Background())
	if res.Ran {
		t.Error("the tick ran again after giving up")
	}
	if len(n.sent) != before {
		t.Error("another message was sent after the give-up notice")
	}
}

// TestTickUnreadableState checks a corrupt state file cannot cause a permanent
// silence. One duplicate message is far better than none at all.
func TestTickUnreadableState(t *testing.T) {
	n := &fakeNotifier{}
	b := newTestBrief(&fakeTimetable{services: usualServices()},
		&fakeDelays{delays: map[string]int{}}, &fakeRenderer{}, n)
	st := &fakeState{loadErr: errBoom}

	res, err := newTestTick(at("07:50"), st, b).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Ran {
		t.Error("an unreadable state file must not suppress the brief")
	}
	if len(n.sent) != 1 {
		t.Errorf("sent %d messages, want 1", len(n.sent))
	}
}

// TestTickStateSaveFailure checks a failed save does not retract a brief that
// has already gone out.
func TestTickStateSaveFailure(t *testing.T) {
	n := &fakeNotifier{}
	b := newTestBrief(&fakeTimetable{services: usualServices()},
		&fakeDelays{delays: map[string]int{}}, &fakeRenderer{}, n)
	st := &fakeState{saveErr: errBoom}

	res, err := newTestTick(at("07:50"), st, b).Run(context.Background())
	if err != nil {
		t.Fatalf("a state write failure must not fail the run: %v", err)
	}
	if !res.Ran || len(n.sent) != 1 {
		t.Error("the brief should still have been delivered")
	}
}

// TestTickDryRunLeavesNoTrace checks -dry-run does not consume the day's
// delivery slot, so a debugging session at 07:50 cannot suppress the real one.
func TestTickDryRunLeavesNoTrace(t *testing.T) {
	n := &fakeNotifier{}
	b := newTestBrief(&fakeTimetable{services: usualServices()},
		&fakeDelays{delays: map[string]int{}}, &fakeRenderer{}, n)
	st := &fakeState{}

	tick := newTestTick(at("07:50"), st, b)
	tick.DryRun = true

	res, err := tick.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Ran {
		t.Error("a dry run should still compute the brief")
	}
	if st.saves != 0 {
		t.Errorf("a dry run wrote state %d times, want 0", st.saves)
	}
}
