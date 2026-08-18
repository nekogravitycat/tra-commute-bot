package domain

import (
	"sort"
	"time"
)

// Compensation is one "leave earlier" option from §7.8: a train that is
// currently out of reach only because of the assumed T_ready, together with
// what it costs and what it buys.
type Compensation struct {
	Candidate Candidate
	// RequiredReady is when the user would have to be at the station.
	RequiredReady time.Time
	// EarlyLeave is how much earlier than planned that is — the cost.
	EarlyLeave time.Duration
	// Lateness is the resulting lateness — the benefit.
	Lateness time.Duration
	// Saved is how much lateness this removes versus doing nothing.
	Saved time.Duration
}

// EarlyLeaveMinutes returns the cost in whole minutes.
func (c Compensation) EarlyLeaveMinutes() int { return int(c.EarlyLeave / time.Minute) }

// LatenessMinutes returns the resulting lateness in whole minutes.
func (c Compensation) LatenessMinutes() int { return int(c.Lateness / time.Minute) }

// SavedMinutes returns the minutes of lateness avoided.
func (c Compensation) SavedMinutes() int { return int(c.Saved / time.Minute) }

// FindCompensations searches for trains that become boardable if the user
// leaves earlier, per §7.8.
//
// It only considers MISSED and RISKY trains, because a CATCHABLE one is already
// available at the current T_ready and ranked accordingly. The search space is
// at most a handful of trains, so a linear scan is the whole algorithm.
//
// The result is ordered so the caller can simply take the first: least lateness
// first, and among equally-late options the one that costs the least sleep.
func FindCompensations(plan Plan, p Params, maxEarlyLeave time.Duration) []Compensation {
	if plan.Recommended == nil {
		return nil
	}
	baseline := *plan.Recommended

	var out []Compensation
	for _, c := range plan.Candidates {
		if c.Catchability == Catchable {
			continue
		}
		// Leaving earlier is only worth proposing if it actually gets the
		// user to the destination sooner than doing nothing.
		if !c.EstArr.Before(baseline.EstArr) {
			continue
		}

		requiredReady := c.EstDep.Add(-p.BoardBuffer).Add(-p.RiskMargin)
		earlyLeave := p.Ready.Sub(requiredReady)
		if earlyLeave <= 0 || earlyLeave > maxEarlyLeave {
			continue
		}

		lateness := p.LatenessFor(c.EstArr)
		out = append(out, Compensation{
			Candidate:     c,
			RequiredReady: requiredReady,
			EarlyLeave:    earlyLeave,
			Lateness:      lateness,
			Saved:         baseline.Lateness - lateness,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Lateness != out[j].Lateness {
			return out[i].Lateness < out[j].Lateness
		}
		// Among options that land the user equally late — most importantly
		// among several that make them on time — the cheapest one wins.
		if out[i].EarlyLeave != out[j].EarlyLeave {
			return out[i].EarlyLeave < out[j].EarlyLeave
		}
		return out[i].Candidate.TrainNo < out[j].Candidate.TrainNo
	})
	return out
}
