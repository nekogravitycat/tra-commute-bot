package domain

import (
	"testing"
	"time"
)

// TestCompensationRescues covers the case the specification calls the only
// genuinely valuable compensation: a train that is out of reach purely because
// of the assumed T_ready, and that turns a late morning into a punctual one for
// the price of a few minutes' sleep.
func TestCompensationRescues(t *testing.T) {
	// 1136 is delayed 8 minutes (departs 08:24) so it needs the user at the
	// platform by 08:19 — one minute earlier than planned. 2008 is delayed 24
	// and lands the user 16 minutes late.
	plan := buildTestPlan(t, usualServices(), map[string]int{"1136": 8, "2008": 24, "1138": 2})
	p := testParams()

	if got := plan.Recommended.TrainNo; got != "1138" {
		t.Fatalf("baseline recommendation = %s, want 1138", got)
	}

	comps := FindCompensations(plan, p, 15*time.Minute)
	if len(comps) == 0 {
		t.Fatal("no compensation found, but leaving one minute earlier catches 1136")
	}

	best := comps[0]
	if best.Candidate.TrainNo != "1136" {
		t.Errorf("best compensation = %s, want 1136", best.Candidate.TrainNo)
	}
	if best.EarlyLeaveMinutes() != 1 {
		t.Errorf("early leave = %d min, want 1", best.EarlyLeaveMinutes())
	}
	if !best.RequiredReady.Equal(at("08:19")) {
		t.Errorf("required ready = %s, want 08:19", best.RequiredReady.Format("15:04"))
	}
	// 1136 arrives 09:05, clocks in 09:25: on time.
	if best.LatenessMinutes() != 0 {
		t.Errorf("resulting lateness = %d min, want 0", best.LatenessMinutes())
	}
	if best.SavedMinutes() != plan.Recommended.LatenessMinutes() {
		t.Errorf("saved = %d min, want the full baseline lateness of %d",
			best.SavedMinutes(), plan.Recommended.LatenessMinutes())
	}
}

// TestCompensationBeyondReach checks the search does not invent options the
// user has already said they will not take.
func TestCompensationBeyondReach(t *testing.T) {
	// An early train that would need the user at the station 25 minutes
	// sooner, well past the configured tolerance.
	services := []Service{
		svc("5001", "1131", "區間", "07:55", "08:35"),
		svc("2008", "1132", "區間快", "08:26", "09:02"),
	}
	plan := buildTestPlan(t, services, map[string]int{"5001": 0, "2008": 40})

	comps := FindCompensations(plan, testParams(), 15*time.Minute)
	for _, c := range comps {
		if c.Candidate.TrainNo == "5001" {
			t.Errorf("proposed leaving %d minutes early, beyond the 15 minute limit",
				c.EarlyLeaveMinutes())
		}
	}
}

// TestCompensationHopeless is the boundary the specification is explicit about:
// when nothing helps, the system must say so rather than manufacture a plan.
func TestCompensationHopeless(t *testing.T) {
	// Every train equally delayed: leaving earlier changes which train is
	// reachable but never produces an earlier arrival than the baseline.
	plan := buildTestPlan(t, usualServices(), map[string]int{"1136": 40, "2008": 40, "1138": 40})

	if got := FindCompensations(plan, testParams(), 15*time.Minute); len(got) != 0 {
		t.Errorf("invented %d compensation options where none helps: %v",
			len(got), got[0].Candidate.TrainNo)
	}
}

// TestCompensationIgnoresLaterArrivals checks an option must actually get the
// user in sooner; getting up early for nothing is not a plan.
func TestCompensationIgnoresLaterArrivals(t *testing.T) {
	services := []Service{
		// Missed, and slower: reachable if the user hurries, but it still
		// gets in after the baseline train does.
		svc("5002", "1131", "區間", "08:18", "09:20"),
		svc("2008", "1132", "區間快", "08:26", "09:02"),
	}
	// 2008 runs 10 late: arrives 09:12, two minutes past the deadline once
	// the last mile is added. 5002 arrives 09:20 — earlier departure, later
	// arrival, so getting up early buys nothing.
	plan := buildTestPlan(t, services, map[string]int{"5002": 0, "2008": 10})

	if got := plan.Recommended.TrainNo; got != "2008" {
		t.Fatalf("baseline = %s, want 2008", got)
	}

	for _, c := range FindCompensations(plan, testParams(), 15*time.Minute) {
		if c.Candidate.TrainNo == "5002" {
			t.Error("proposed an earlier departure that arrives later than doing nothing")
		}
	}
}

// TestCompensationOrdering pins the ordering rule: least lateness first, and
// among options that land equally, the one that costs the least sleep.
func TestCompensationOrdering(t *testing.T) {
	services := []Service{
		svc("4001", "1131", "區間", "08:10", "08:50"), // needs 08:05, on time
		svc("4002", "1131", "區間", "08:16", "08:50"), // needs 08:11, on time too
		svc("2008", "1132", "區間快", "08:26", "09:02"),
	}
	plan := buildTestPlan(t, services, map[string]int{"4001": 0, "4002": 0, "2008": 30})

	comps := FindCompensations(plan, testParams(), 15*time.Minute)
	if len(comps) < 2 {
		t.Fatalf("expected both early trains as options, got %d", len(comps))
	}
	if comps[0].Candidate.TrainNo != "4002" {
		t.Errorf("first option = %s, want 4002: both arrive together, so the "+
			"one needing less of a head start wins", comps[0].Candidate.TrainNo)
	}
}

// TestCompensationNoRecommendation checks the search is a no-op when there is
// no baseline to improve on.
func TestCompensationNoRecommendation(t *testing.T) {
	if got := FindCompensations(Plan{}, testParams(), 15*time.Minute); got != nil {
		t.Errorf("compensations = %v, want none when there is no recommendation", got)
	}
}
