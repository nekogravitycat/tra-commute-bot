package domain

import (
	"sort"
	"time"
)

// BoardRow is one train on a shortcut's live board (§10.x): a Service after
// live delay data has been applied, same as Candidate, but with no
// catchability or lateness — a shortcut has no ready time or deadline to
// classify against.
type BoardRow struct {
	Service

	Delay       time.Duration // clamped at zero; trains do not leave early
	DelaySource DelaySource

	EstDep time.Time // SchedDep + Delay
	EstArr time.Time // SchedArr + Delay
}

// DelayMinutes returns the delay rounded to whole minutes, the unit the
// message displays.
func (r BoardRow) DelayMinutes() int { return int(r.Delay / time.Minute) }

// Board is the unranked outcome of BuildBoard: every train still worth
// showing on a shortcut's live board, in departure order.
type Board struct {
	// Rows is every surviving train, sorted by estimated departure.
	Rows []BoardRow
	// SuspendedCount counts trains dropped for being suspended, for the
	// message's footnote rather than a row of its own.
	SuspendedCount int
	// ExcludedCount counts trains dropped for ticket ineligibility (A13),
	// the same rule BuildPlan applies.
	ExcludedCount int
	// UnknownTypes lists type names that matched neither the exclusion list
	// nor a known keyword, so the message can warn about them.
	UnknownTypes []string
}

// BuildBoard answers "what is running on this route right now" rather than
// BuildPlan's "what should I catch": it applies the same live-delay and
// ticket-eligibility rules, but does no window filtering, no catchability
// classification and no ranking, because a shortcut has no ready time or
// deadline to evaluate any of those against. Rows still departing (estimated
// departure at or after now) are kept, sorted earliest first.
func BuildBoard(services []Service, delays map[string]int, filter TypeFilter, now time.Time) Board {
	var b Board
	seenUnknown := map[string]bool{}

	for _, svc := range services {
		if svc.Suspended {
			b.SuspendedCount++
			continue
		}

		switch filter.Eligibility(svc.TypeID, svc.TypeName) {
		case TicketIneligible:
			b.ExcludedCount++
			continue
		case TicketUnknown:
			if !seenUnknown[svc.TypeName] {
				seenUnknown[svc.TypeName] = true
				b.UnknownTypes = append(b.UnknownTypes, svc.TypeName)
			}
			if filter.Policy == ExcludeUnknown {
				b.ExcludedCount++
				continue
			}
		}

		row := BoardRow{Service: svc}
		if raw, ok := delays[svc.TrainNo]; ok {
			row.Delay = ClampDelay(raw)
			row.DelaySource = DelaySourceLive
		}
		row.EstDep = svc.SchedDep.Add(row.Delay)
		row.EstArr = svc.SchedArr.Add(row.Delay)

		if row.EstDep.Before(now) {
			continue
		}
		b.Rows = append(b.Rows, row)
	}

	sort.SliceStable(b.Rows, func(i, j int) bool { return b.Rows[i].EstDep.Before(b.Rows[j].EstDep) })
	return b
}
