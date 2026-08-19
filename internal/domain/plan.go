package domain

import (
	"sort"
	"time"
)

// Window is the §7.2 scan range around T_ready, expressed as offsets.
type Window struct {
	// Lookback reaches backwards from T_ready. It is not redundant: a train
	// whose scheduled departure has already passed may still be sitting at
	// the platform because it is running late, and that is exactly the case
	// the whole system exists to catch.
	Lookback time.Duration
	// Lookahead reaches forwards from T_ready.
	Lookahead time.Duration
}

// PlanInput is everything BuildPlan needs. Gathering it into one struct keeps
// the signature stable as the algorithm grows and makes the table-driven tests
// readable.
type PlanInput struct {
	Services      []Service
	Delays        map[string]int // train number -> raw delay in minutes
	Params        Params
	Window        Window
	Filter        TypeFilter
	UsualTrainNos []string
}

// Plan is the ranked outcome of §7.5.
// Plan is built once by BuildPlan and then treated as immutable — Recommended
// and BestRisky point into Candidates' backing array rather than holding
// copies, which only stays safe because nothing appends to Candidates after
// BuildPlan returns. Do not mutate a Plan's Candidates in place after it is
// built.
type Plan struct {
	// Candidates is every surviving train, ranked best first.
	Candidates []Candidate
	// Recommended is the top-ranked train, or nil if nothing survived.
	Recommended *Candidate
	// Alternatives are the runners-up, capped by the caller.
	Alternatives []Candidate
	// BestRisky is the highest-ranked RISKY train that is not the
	// recommendation, surfaced so the message can present the gamble
	// explicitly instead of quietly discarding it (§7.7 scenario D).
	BestRisky *Candidate
	// Excluded counts trains dropped for ticket ineligibility or suspension,
	// for the log rather than the message.
	ExcludedCount  int
	SuspendedCount int
	// UnknownTypes lists type names that matched nothing, so they can be
	// logged and folded into the config later.
	UnknownTypes []string
}

// Empty reports whether the plan found nothing to recommend.
func (p Plan) Empty() bool { return p.Recommended == nil }

// BuildPlan runs §7.2 through §7.5: window filter, ticket-type filter, live
// delay application, two-axis classification, then the lexicographic ranking.
func BuildPlan(in PlanInput) Plan {
	usual := make(map[string]bool, len(in.UsualTrainNos))
	for _, no := range in.UsualTrainNos {
		usual[no] = true
	}

	from := in.Params.Ready.Add(-in.Window.Lookback)
	to := in.Params.Ready.Add(in.Window.Lookahead)

	var plan Plan
	seenUnknown := map[string]bool{}

	for _, svc := range in.Services {
		if svc.SchedDep.Before(from) || svc.SchedDep.After(to) {
			continue
		}
		if svc.Suspended {
			plan.SuspendedCount++
			continue
		}

		unknown := false
		switch in.Filter.Eligibility(svc.TypeID, svc.TypeName) {
		case TicketIneligible:
			plan.ExcludedCount++
			continue
		case TicketUnknown:
			if !seenUnknown[svc.TypeName] {
				seenUnknown[svc.TypeName] = true
				plan.UnknownTypes = append(plan.UnknownTypes, svc.TypeName)
			}
			if in.Filter.Policy == ExcludeUnknown {
				plan.ExcludedCount++
				continue
			}
			unknown = true
		}

		plan.Candidates = append(plan.Candidates, newCandidate(svc, in, usual[svc.TrainNo], unknown))
	}

	sortCandidates(plan.Candidates)

	if len(plan.Candidates) > 0 {
		plan.Recommended = &plan.Candidates[0]
		plan.Alternatives = plan.Candidates[1:]
		for i := 1; i < len(plan.Candidates); i++ {
			if plan.Candidates[i].Catchability == Risky {
				plan.BestRisky = &plan.Candidates[i]
				break
			}
		}
	}
	return plan
}

func newCandidate(svc Service, in PlanInput, usual, unknown bool) Candidate {
	c := Candidate{Service: svc, Usual: usual, UnknownType: unknown}

	// §7.3: apply the live delay, clamped at zero, and propagate it
	// unchanged to the arrival. Assuming no recovery is the conservative
	// direction, and the two errors are not symmetric here: overstating a
	// delay makes the user early, understating it makes them late.
	if raw, ok := in.Delays[svc.TrainNo]; ok {
		c.Delay = ClampDelay(raw)
		c.DelaySource = DelaySourceLive
	}

	c.EstDep = svc.SchedDep.Add(c.Delay)
	c.EstArr = svc.SchedArr.Add(c.Delay)
	c.Catchability = Classify(c.EstDep, in.Params)
	c.Lateness = in.Params.LatenessFor(c.EstArr)
	return c
}

// sortCandidates applies the §7.5 lexicographic ranking. A weighted score was
// rejected deliberately: the user's preference is strictly layered ("first make
// sure I can board, then get me there earliest"), and a lexicographic order
// reproduces that exactly while staying explainable on the day it surprises
// someone.
func sortCandidates(cs []Candidate) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]

		// 1. Catchability, best first. A risky on-time train is worth less
		//    than a safe two-minutes-late one, because missing it means
		//    falling back to something later still.
		if a.Catchability != b.Catchability {
			return a.Catchability > b.Catchability
		}
		// 2. Earliest estimated arrival. Within a catchability tier this is
		//    also, by construction, the minimum-lateness order: lateness is a
		//    monotonic function of arrival, so listing it separately would
		//    add a sort key that can never change the result.
		if !a.EstArr.Equal(b.EstArr) {
			return a.EstArr.Before(b.EstArr)
		}
		// 3. Latest departure, to spend less time waiting on the platform.
		if !a.EstDep.Equal(b.EstDep) {
			return a.EstDep.After(b.EstDep)
		}
		// 4. Prefer a train whose delay was actually observed.
		if a.DelaySource != b.DelaySource {
			return a.DelaySource > b.DelaySource
		}
		// 5. Train number, so the same inputs always give the same output.
		return a.TrainNo < b.TrainNo
	})
}

// TopAlternatives returns at most n alternatives.
func (p Plan) TopAlternatives(n int) []Candidate {
	if n < 0 || n > len(p.Alternatives) {
		n = len(p.Alternatives)
	}
	return p.Alternatives[:n]
}
