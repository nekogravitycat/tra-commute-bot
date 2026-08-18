package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

func TestBriefRunHappyPath(t *testing.T) {
	tt := &fakeTimetable{services: usualServices()}
	d := &fakeDelays{delays: map[string]int{"1136": 10, "2008": 10, "1138": 10}}
	r, n := &fakeRenderer{}, &fakeNotifier{}

	res, err := newTestBrief(tt, d, r, n).Run(context.Background(), at("07:50"), "平日通勤", testTrip(), testUsualTrainNos())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !res.Delivered {
		t.Error("the brief was computed but never delivered")
	}
	if len(n.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(n.sent))
	}
	if res.Brief.Mode != domain.ModeNormal {
		t.Errorf("mode = %v, want normal", res.Brief.Mode)
	}
	// A uniform ten minute delay makes the earlier local train both catchable
	// and the first to arrive.
	if got := res.Brief.Plan.Recommended.TrainNo; got != "1136" {
		t.Errorf("recommended = %s, want 1136", got)
	}
	if !res.Brief.LiveDataAvailable {
		t.Error("live data was available and should be reported as such")
	}
}

// TestBriefParamsDeriveFromFiredAt checks the plan is anchored to the schedule's
// nominal time, not to the moment the process happened to wake up. A tick that
// lands a minute late must still plan the same morning.
func TestBriefParamsDeriveFromFiredAt(t *testing.T) {
	tt := &fakeTimetable{services: usualServices()}
	d := &fakeDelays{delays: map[string]int{}}
	r, n := &fakeRenderer{}, &fakeNotifier{}

	res, err := newTestBrief(tt, d, r, n).Run(context.Background(), at("07:50"), "平日通勤", testTrip(), testUsualTrainNos())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !res.Brief.Params.Ready.Equal(at("08:20")) {
		t.Errorf("T_ready = %s, want 08:20 (07:50 plus the 30 minute lead)",
			res.Brief.Params.Ready.Format("15:04"))
	}
	if !res.Brief.Params.Deadline.Equal(at("09:30")) {
		t.Errorf("deadline = %s, want 09:30", res.Brief.Params.Deadline.Format("15:04"))
	}
}

// TestBriefTimetableFailure covers the §9.3 escalation: with no timetable there
// is nothing to salvage, but a warning still goes out.
func TestBriefTimetableFailure(t *testing.T) {
	tt := &fakeTimetable{err: errBoom}
	d := &fakeDelays{delays: map[string]int{}}
	r, n := &fakeRenderer{}, &fakeNotifier{}

	res, err := newTestBrief(tt, d, r, n).Run(context.Background(), at("07:50"), "平日通勤", testTrip(), testUsualTrainNos())
	if err != nil {
		t.Fatalf("a data failure must not become a run failure: %v", err)
	}

	if res.Brief.Mode != domain.ModeDegraded {
		t.Errorf("mode = %v, want degraded", res.Brief.Mode)
	}
	if !res.Delivered || len(n.sent) != 1 {
		t.Error("a degraded brief must still be delivered; silence is the one unacceptable outcome")
	}
	if res.Brief.Degradation == nil || res.Brief.Degradation.Stage != "timetable" {
		t.Errorf("degradation = %+v, want the timetable stage", res.Brief.Degradation)
	}
	// With no timetable there is no point asking for delays.
	if d.calls != 0 {
		t.Errorf("live board called %d times despite having no timetable", d.calls)
	}
}

// TestBriefLiveFailureKeepsTimetable covers the survivable half of §9.3: the
// scheduled times are still worth sending, as long as the message says plainly
// that no delays have been applied.
func TestBriefLiveFailureKeepsTimetable(t *testing.T) {
	tt := &fakeTimetable{services: usualServices()}
	d := &fakeDelays{err: errBoom}
	r, n := &fakeRenderer{}, &fakeNotifier{}

	res, err := newTestBrief(tt, d, r, n).Run(context.Background(), at("07:50"), "平日通勤", testTrip(), testUsualTrainNos())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Brief.Mode != domain.ModeDegraded {
		t.Errorf("mode = %v, want degraded", res.Brief.Mode)
	}
	if res.Brief.LiveDataAvailable {
		t.Error("the brief must not claim live data it never received")
	}
	if len(res.Brief.Plan.Candidates) == 0 {
		t.Error("the scheduled times survived and should still be sent")
	}
	for _, c := range res.Brief.Plan.Candidates {
		if c.DelaySource != domain.DelaySourceNone {
			t.Errorf("%s claims a delay source with no live data", c.TrainNo)
		}
	}
	if !res.Delivered {
		t.Error("the salvaged brief was not delivered")
	}
}

// TestBriefSendRetries checks delivery is retried before the run is declared
// failed, since a transient network blip should not cost the whole morning.
func TestBriefSendRetries(t *testing.T) {
	tt := &fakeTimetable{services: usualServices()}
	d := &fakeDelays{delays: map[string]int{}}
	r := &fakeRenderer{}
	n := &fakeNotifier{failFor: 2}

	var waits []time.Duration
	b := newTestBrief(tt, d, r, n)
	b.SendBackoff = time.Second
	b.Sleep = func(w time.Duration) { waits = append(waits, w) }

	res, err := b.Run(context.Background(), at("07:50"), "平日通勤", testTrip(), testUsualTrainNos())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Delivered {
		t.Error("delivery should have succeeded on the third attempt")
	}
	if n.attempts != 3 {
		t.Errorf("attempts = %d, want 3", n.attempts)
	}
	// The wait doubles, so a struggling endpoint is not hammered.
	want := []time.Duration{time.Second, 2 * time.Second}
	if len(waits) != len(want) {
		t.Fatalf("waits = %v, want %v", waits, want)
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Errorf("wait %d = %v, want %v", i, waits[i], want[i])
		}
	}
}

// TestBriefSendExhausted checks a total delivery failure is reported as an
// error, which is what gives the process a non-zero exit code for journald.
func TestBriefSendExhausted(t *testing.T) {
	tt := &fakeTimetable{services: usualServices()}
	d := &fakeDelays{delays: map[string]int{}}
	r := &fakeRenderer{}
	n := &fakeNotifier{failFor: 99}

	res, err := newTestBrief(tt, d, r, n).Run(context.Background(), at("07:50"), "平日通勤", testTrip(), testUsualTrainNos())
	if err == nil {
		t.Fatal("expected an error once every delivery attempt failed")
	}
	if res.Delivered {
		t.Error("Delivered must be false when nothing was sent")
	}
	if n.attempts != 4 {
		t.Errorf("attempts = %d, want 4 (the initial send plus three retries)", n.attempts)
	}
}

func TestRunDegraded(t *testing.T) {
	tt := &fakeTimetable{services: usualServices()}
	d := &fakeDelays{delays: map[string]int{}}
	r, n := &fakeRenderer{}, &fakeNotifier{}

	res, err := newTestBrief(tt, d, r, n).RunDegraded(
		context.Background(), at("07:50"), "平日通勤", "retry window expired", testTrip())
	if err != nil {
		t.Fatalf("RunDegraded: %v", err)
	}

	if res.Brief.Mode != domain.ModeDegraded {
		t.Errorf("mode = %v, want degraded", res.Brief.Mode)
	}
	if !res.Delivered {
		t.Error("the give-up notice must go out")
	}
	// The give-up path fetches nothing; it exists precisely because fetching
	// has already failed repeatedly.
	if tt.calls != 0 || d.calls != 0 {
		t.Errorf("give-up path made %d/%d API calls, want none", tt.calls, d.calls)
	}
}

// TestBriefDeadlineRollover covers the defensive branch for a schedule late
// enough in the day that a wall-clock deadline belongs to tomorrow.
func TestBriefDeadlineRollover(t *testing.T) {
	tt := &fakeTimetable{services: usualServices()}
	d := &fakeDelays{delays: map[string]int{}}
	r, n := &fakeRenderer{}, &fakeNotifier{}

	b := newTestBrief(tt, d, r, n)
	// A "夜班" schedule with a ready time just after midnight and a deadline
	// early the same wall-clock morning: the deadline must roll to the next
	// calendar day to stay after Ready.
	nightTrip := testTrip()
	nightTrip.ReadyAt = domain.TimeOfDay{Hour: 23, Minute: 30}
	nightTrip.DeadlineAt = domain.TimeOfDay{Hour: 0, Minute: 30}
	res, err := b.Run(context.Background(), at("23:00"), "夜班", nightTrip, testUsualTrainNos())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	p := res.Brief.Params
	if !p.Deadline.After(p.Ready) {
		t.Errorf("deadline %s precedes T_ready %s", p.Deadline, p.Ready)
	}
	if got := p.Deadline.Day(); got != 19 {
		t.Errorf("deadline day = %d, want 19 (the following day)", got)
	}
}
