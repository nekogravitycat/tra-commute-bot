package domain

import (
	"testing"
	"time"
)

// TestScenarios covers §7.7 A–E, the five cases the specification requires to
// pass before the algorithm can be trusted.
func TestScenarios(t *testing.T) {
	tests := []struct {
		name            string
		delays          map[string]int
		wantRecommended string
		wantOrder       []string
		wantClass       map[string]Catchability
		wantLateness    map[string]int
	}{
		{
			// Scenario A: nothing is delayed, so the answer is the everyday
			// one — 1136 has already gone, 2008 is the best remaining train.
			name:            "A/all on time",
			delays:          map[string]int{"1136": 0, "2008": 0, "1138": 0},
			wantRecommended: "2008",
			wantOrder:       []string{"2008", "1138", "1136"},
			wantClass: map[string]Catchability{
				"1136": Missed, "2008": Catchable, "1138": Catchable,
			},
			wantLateness: map[string]int{"2008": 0, "1138": 4},
		},
		{
			// Scenario B: a uniform ten-minute delay makes 1136 catchable,
			// and it still reaches Taipei five minutes before 2008 because it
			// left ten minutes earlier. This is the case where the intuition
			// "the express is faster" gives the wrong answer, and the only
			// way to get it right is to compare estimated arrivals.
			name:            "B/uniform ten minute delay",
			delays:          map[string]int{"1136": 10, "2008": 10, "1138": 10},
			wantRecommended: "1136",
			wantOrder:       []string{"1136", "2008", "1138"},
			wantClass: map[string]Catchability{
				"1136": Catchable, "2008": Catchable, "1138": Catchable,
			},
			wantLateness: map[string]int{"1136": 0, "2008": 2, "1138": 14},
		},
		{
			// Scenario C: the usual first choice is badly delayed while the
			// earlier train is only slightly late. This is the case the whole
			// system exists for.
			name:            "C/uneven delays",
			delays:          map[string]int{"1136": 10, "2008": 25, "1138": 2},
			wantRecommended: "1136",
			wantClass: map[string]Catchability{
				"1136": Catchable, "2008": Catchable, "1138": Catchable,
			},
			wantLateness: map[string]int{"1136": 0, "2008": 17},
		},
		{
			// Scenario D: 1136 becomes catchable only by two minutes, which
			// is exactly the situation that must not become a recommendation:
			// the delay it depends on can shrink while the user is walking.
			name:            "D/boundary, catchable only on the delay",
			delays:          map[string]int{"1136": 6, "2008": 6, "1138": 6},
			wantRecommended: "2008",
			wantClass: map[string]Catchability{
				"1136": Risky, "2008": Catchable, "1138": Catchable,
			},
		},
		{
			// Scenario E: everything is late, so the answer is whichever
			// train loses the least time.
			name:            "E/late whatever happens",
			delays:          map[string]int{"1136": 30, "2008": 30, "1138": 30},
			wantRecommended: "1136",
			wantOrder:       []string{"1136", "2008", "1138"},
			wantLateness:    map[string]int{"1136": 17, "2008": 22, "1138": 34},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := buildTestPlan(t, usualServices(), tc.delays)

			if plan.Recommended == nil {
				t.Fatal("no recommendation")
			}
			if got := plan.Recommended.TrainNo; got != tc.wantRecommended {
				t.Errorf("recommended = %s, want %s", got, tc.wantRecommended)
			}
			if tc.wantOrder != nil {
				got := trainNos(plan.Candidates)
				if len(got) != len(tc.wantOrder) {
					t.Fatalf("ranking = %v, want %v", got, tc.wantOrder)
				}
				for i := range got {
					if got[i] != tc.wantOrder[i] {
						t.Fatalf("ranking = %v, want %v", got, tc.wantOrder)
					}
				}
			}
			for no, want := range tc.wantClass {
				if got := find(t, plan, no).Catchability; got != want {
					t.Errorf("%s catchability = %v, want %v", no, got, want)
				}
			}
			for no, want := range tc.wantLateness {
				if got := find(t, plan, no).LatenessMinutes(); got != want {
					t.Errorf("%s lateness = %d min, want %d", no, got, want)
				}
			}
		})
	}
}

// TestScenarioD_BestRisky checks that the near-miss train is kept and surfaced
// rather than silently dropped: the user, not the program, decides whether to
// gamble on a delay holding.
func TestScenarioD_BestRisky(t *testing.T) {
	plan := buildTestPlan(t, usualServices(), map[string]int{"1136": 6, "2008": 6, "1138": 6})
	if plan.BestRisky == nil {
		t.Fatal("risky train was not surfaced")
	}
	if plan.BestRisky.TrainNo != "1136" {
		t.Errorf("best risky = %s, want 1136", plan.BestRisky.TrainNo)
	}
}

func TestClassifyBoundaries(t *testing.T) {
	p := testParams() // catchable from 08:25, boardable from 08:22

	tests := []struct {
		estDep string
		want   Catchability
	}{
		{"08:21", Missed},
		{"08:22", Risky}, // exactly the earliest boarding instant
		{"08:24", Risky},
		{"08:25", Catchable}, // exactly the safe-boarding instant
		{"08:26", Catchable},
	}
	for _, tc := range tests {
		if got := Classify(at(tc.estDep), p); got != tc.want {
			t.Errorf("Classify(%s) = %v, want %v", tc.estDep, got, tc.want)
		}
	}
}

// TestLatenessExactlyZero pins the boundary: arriving exactly on the deadline
// is on time, and must not flip the message into the warning template.
func TestLatenessExactlyZero(t *testing.T) {
	p := testParams()
	// 09:10 arrival + 20 minutes last mile = 09:30, exactly the deadline.
	if got := p.LatenessFor(at("09:10")); got != 0 {
		t.Errorf("lateness at the deadline = %v, want 0", got)
	}
	if got := p.LatenessFor(at("09:11")); got != time.Minute {
		t.Errorf("lateness one minute past = %v, want 1m", got)
	}
	if got := p.SlackFor(at("09:10")); got != 0 {
		t.Errorf("slack at the deadline = %v, want 0", got)
	}
}

// TestNegativeDelayClamped guards the defensive rule: TRA has been reported to
// publish negative delays, and a train that leaves early would corrupt every
// downstream estimate.
func TestNegativeDelayClamped(t *testing.T) {
	plan := buildTestPlan(t, usualServices(), map[string]int{"2008": -7})
	c := find(t, plan, "2008")
	if c.Delay != 0 {
		t.Errorf("delay = %v, want 0", c.Delay)
	}
	if !c.EstDep.Equal(at("08:26")) {
		t.Errorf("estimated departure = %s, want the scheduled 08:26", c.EstDep.Format("15:04"))
	}
}

// TestNoLiveData checks a train with no live record still ranks, but loses the
// tie-break to one whose delay was actually observed.
func TestNoLiveData(t *testing.T) {
	plan := buildTestPlan(t, usualServices(), map[string]int{"1136": 0, "1138": 0})

	c := find(t, plan, "2008")
	if c.DelaySource != DelaySourceNone {
		t.Errorf("delay source = %v, want none", c.DelaySource)
	}
	if c.Delay != 0 {
		t.Errorf("delay = %v, want 0", c.Delay)
	}

	// Two trains arriving and departing together, one observed and one not.
	services := []Service{
		svc("9001", "1131", "區間", "08:30", "09:00"),
		svc("9002", "1131", "區間", "08:30", "09:00"),
	}
	plan = buildTestPlan(t, services, map[string]int{"9002": 0})
	if got := plan.Recommended.TrainNo; got != "9002" {
		t.Errorf("recommended = %s, want 9002 (the one with live data)", got)
	}
}

func TestTrainTypeFiltering(t *testing.T) {
	services := []Service{
		svc("2008", "1132", "區間快", "08:26", "09:02"),
		svc("278", "110H", "自強(3000)", "08:55", "09:27"),
		svc("602", "1111", "莒光", "08:40", "09:30"),
		svc("272", "1107", "普悠瑪", "08:30", "09:00"),
		svc("408", "1101", "太魯閣", "08:32", "09:01"),
	}
	plan := buildTestPlan(t, services, map[string]int{})

	for _, no := range []string{"2008", "278", "602"} {
		find(t, plan, no) // fails the test if absent
	}
	for _, c := range plan.Candidates {
		if c.TrainNo == "272" || c.TrainNo == "408" {
			t.Errorf("%s (%s) must be excluded: it does not accept electronic tickets",
				c.TrainNo, c.TypeName)
		}
	}
	if plan.ExcludedCount != 2 {
		t.Errorf("excluded count = %d, want 2", plan.ExcludedCount)
	}
	// Reserved-seat trains carry no extra marking: a standing ticket on a
	// stored value card is a perfectly ordinary way to ride them.
	if find(t, plan, "278").UnknownType {
		t.Error("自強(3000) must not be flagged as unknown")
	}
}

// TestExcludedByKeyword covers the defensive fallback for the day TRA renumbers
// a train type and the ID list goes stale.
func TestExcludedByKeyword(t *testing.T) {
	services := []Service{
		svc("2008", "1132", "區間快", "08:26", "09:02"),
		svc("999", "9999", "普悠瑪(新型)", "08:30", "09:00"),
	}
	plan := buildTestPlan(t, services, map[string]int{})
	for _, c := range plan.Candidates {
		if c.TrainNo == "999" {
			t.Error("a renumbered 普悠瑪 must still be excluded by name")
		}
	}
}

func TestUnknownTrainType(t *testing.T) {
	services := []Service{
		svc("2008", "1132", "區間快", "08:26", "09:02"),
		svc("777", "9999", "磁浮特快", "08:30", "09:00"),
	}

	t.Run("include_and_flag", func(t *testing.T) {
		plan := buildTestPlan(t, services, map[string]int{})
		c := find(t, plan, "777")
		if !c.UnknownType {
			t.Error("unknown type must be flagged so the message can warn")
		}
		if len(plan.UnknownTypes) != 1 || plan.UnknownTypes[0] != "磁浮特快" {
			t.Errorf("unknown types = %v, want [磁浮特快]", plan.UnknownTypes)
		}
	})

	t.Run("exclude", func(t *testing.T) {
		f := testFilter()
		f.Policy = ExcludeUnknown
		plan := BuildPlan(PlanInput{
			Services: services,
			Delays:   map[string]int{},
			Params:   testParams(),
			Window:   Window{Lookback: 30 * time.Minute, Lookahead: 60 * time.Minute},
			Filter:   f,
		})
		for _, c := range plan.Candidates {
			if c.TrainNo == "777" {
				t.Error("unknown type must be dropped under the exclude policy")
			}
		}
	})
}

func TestSuspendedTrainsDropped(t *testing.T) {
	services := usualServices()
	services[1].Suspended = true // 2008 cancelled

	plan := buildTestPlan(t, services, map[string]int{})
	for _, c := range plan.Candidates {
		if c.TrainNo == "2008" {
			t.Error("a cancelled train must never be recommended")
		}
	}
	if plan.SuspendedCount != 1 {
		t.Errorf("suspended count = %d, want 1", plan.SuspendedCount)
	}
}

// TestWindowFiltering checks the lookback reaches trains whose scheduled
// departure has already passed — the whole reason the window is not simply
// "from now on".
func TestWindowFiltering(t *testing.T) {
	services := []Service{
		svc("7000", "1131", "區間", "07:45", "08:30"), // before the lookback
		svc("7001", "1131", "區間", "07:50", "08:35"), // exactly the lookback edge
		svc("7002", "1131", "區間", "09:20", "10:00"), // exactly the lookahead edge
		svc("7003", "1131", "區間", "09:21", "10:01"), // past the lookahead
	}
	plan := buildTestPlan(t, services, map[string]int{})

	got := map[string]bool{}
	for _, c := range plan.Candidates {
		got[c.TrainNo] = true
	}
	if got["7000"] || got["7003"] {
		t.Errorf("trains outside the window were kept: %v", trainNos(plan.Candidates))
	}
	if !got["7001"] || !got["7002"] {
		t.Errorf("trains on the window edges were dropped: %v", trainNos(plan.Candidates))
	}
}

// TestEmptyCandidates checks the degenerate case does not panic and reports
// itself honestly.
func TestEmptyCandidates(t *testing.T) {
	plan := buildTestPlan(t, nil, map[string]int{})
	if !plan.Empty() {
		t.Error("a plan with no services must report itself empty")
	}
	if plan.Recommended != nil {
		t.Error("a plan with no services must not recommend anything")
	}
	if got := plan.TopAlternatives(4); len(got) != 0 {
		t.Errorf("alternatives = %v, want none", got)
	}
}

// TestRankingTieBreaks pins the lower-priority sort keys, which only ever show
// up on days when two trains genuinely coincide.
func TestRankingTieBreaks(t *testing.T) {
	// Same arrival, different departures: the later departure wins, because
	// it means less time standing on the platform.
	services := []Service{
		svc("8001", "1131", "區間", "08:30", "09:05"),
		svc("8002", "1131", "區間", "08:40", "09:05"),
	}
	plan := buildTestPlan(t, services, map[string]int{"8001": 0, "8002": 0})
	if got := plan.Recommended.TrainNo; got != "8002" {
		t.Errorf("recommended = %s, want 8002 (departs later, arrives together)", got)
	}

	// Everything equal: the train number decides, so the same morning always
	// produces the same brief.
	services = []Service{
		svc("8004", "1131", "區間", "08:30", "09:05"),
		svc("8003", "1131", "區間", "08:30", "09:05"),
	}
	plan = buildTestPlan(t, services, map[string]int{"8003": 0, "8004": 0})
	if got := plan.Recommended.TrainNo; got != "8003" {
		t.Errorf("recommended = %s, want 8003 (lowest train number)", got)
	}
}

// TestCatchabilityOutranksLateness pins the top-level rule: a train that is
// safely boardable but slightly late beats one that is punctual but might well
// be missed, because missing it means falling back to something later still.
func TestCatchabilityOutranksLateness(t *testing.T) {
	services := []Service{
		svc("6001", "1131", "區間", "08:22", "09:00"), // risky, on time
		svc("6002", "1131", "區間", "08:30", "09:15"), // catchable, 5 min late
	}
	plan := buildTestPlan(t, services, map[string]int{"6001": 0, "6002": 0})

	if got := plan.Recommended.TrainNo; got != "6002" {
		t.Errorf("recommended = %s, want 6002 (catchable beats risky)", got)
	}
	if plan.Recommended.LatenessMinutes() != 5 {
		t.Errorf("lateness = %d, want 5", plan.Recommended.LatenessMinutes())
	}
	if plan.BestRisky == nil || plan.BestRisky.TrainNo != "6001" {
		t.Error("the punctual risky train must still be offered, not hidden")
	}
}

// TestUsualTrainsMarked checks the habitual trains are identifiable, which is
// what lets the message keep showing them after the ranking demotes them.
func TestUsualTrainsMarked(t *testing.T) {
	plan := buildTestPlan(t, usualServices(), map[string]int{})
	for _, no := range []string{"1136", "2008", "1138"} {
		if !find(t, plan, no).Usual {
			t.Errorf("%s should be marked as a habitual train", no)
		}
	}
}
