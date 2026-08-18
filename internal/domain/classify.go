package domain

import "time"

// Catchability is axis one of the §7.4 model: can the user physically board
// this train? It is deliberately separate from lateness, because "hard to
// catch" and "arrives late" are independent properties and collapsing them
// into one enum makes trains incomparable.
type Catchability int

const (
	// Missed means the train leaves before the user can reach the platform.
	Missed Catchability = iota
	// Risky means it is catchable only just: the margin is thin enough that a
	// delay shrinking while the user is en route would strand them.
	Risky
	// Catchable means there is enough slack to board with confidence.
	Catchable
)

// The iota order above is also the ranking order, so a plain integer
// comparison expresses "CATCHABLE > RISKY > MISSED" from §7.5.

func (c Catchability) String() string {
	switch c {
	case Catchable:
		return "CATCHABLE"
	case Risky:
		return "RISKY"
	default:
		return "MISSED"
	}
}

// Classify places an estimated departure on the catchability axis.
//
// The boundaries are half-open and anchored on Params so that the thresholds
// live in exactly one place:
//
//	MISSED     estDep <  Ready + BoardBuffer
//	RISKY      Ready + BoardBuffer <= estDep < Ready + BoardBuffer + RiskMargin
//	CATCHABLE  estDep >= Ready + BoardBuffer + RiskMargin
func Classify(estDep time.Time, p Params) Catchability {
	switch {
	case estDep.Before(p.EarliestBoarding()):
		return Missed
	case estDep.Before(p.SafeBoarding()):
		return Risky
	default:
		return Catchable
	}
}

// ClampDelay applies the §7.3 defensive rule: TDX has been observed to publish
// negative delays, but a train never leaves ahead of schedule, so anything
// below zero is treated as on time.
func ClampDelay(minutes int) time.Duration {
	if minutes < 0 {
		return 0
	}
	return time.Duration(minutes) * time.Minute
}
