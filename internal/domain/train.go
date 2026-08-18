package domain

import "time"

// DelaySource records where a train's delay figure came from. It matters for
// presentation: a train with no live data must be shown as "表定" rather than
// "+0", so the reader knows the number is an assumption and not an observation.
type DelaySource int

const (
	// DelaySourceNone means no live record was found for this train.
	DelaySourceNone DelaySource = iota
	// DelaySourceLive means the delay came from the live board.
	DelaySourceLive
)

func (d DelaySource) String() string {
	switch d {
	case DelaySourceLive:
		return "live"
	default:
		return "none"
	}
}

// TicketEligibility is the §7.3/A13 verdict on whether the user may board with
// an electronic ticket (悠遊卡). It is deliberately independent of whether the
// train has reserved seating: 自強 and 莒光 are reserved yet boardable with a
// standing ticket, while 普悠瑪 and 太魯閣 are not.
type TicketEligibility int

const (
	// TicketEligible means the train accepts electronic tickets.
	TicketEligible TicketEligibility = iota
	// TicketIneligible means the train is on the exclusion list.
	TicketIneligible
	// TicketUnknown means the train type matched neither the exclusion list
	// nor any recognised type, so the policy in UnknownTypePolicy decides.
	TicketUnknown
)

// Service is one train's scheduled origin-to-destination leg on a single
// service date, with both times already resolved to absolute instants in the
// configured location. Resolving "HH:mm" against the service date (including
// the midnight rollover) is the adapter's job, so the domain never has to
// reason about naive clock strings.
type Service struct {
	TrainNo   string
	TypeID    string
	TypeCode  string
	TypeName  string
	SchedDep  time.Time // departure from the origin station
	SchedArr  time.Time // arrival at the destination station
	Suspended bool      // TrainInfo.SuspendedFlag or either stop's SuspendedFlag
}

// Candidate is a Service after live delay data and the user's constraints have
// been applied. Every field is derived, so a Candidate can be rendered without
// consulting anything else.
type Candidate struct {
	Service

	Delay       time.Duration // clamped at zero; trains do not leave early
	DelaySource DelaySource

	EstDep time.Time // SchedDep + Delay
	EstArr time.Time // SchedArr + Delay

	Catchability Catchability
	// ClockIn is when the user would badge in: EstArr plus the last-mile time.
	ClockIn time.Time
	// Lateness is how late that is, floored at zero (§7.4 axis two).
	Lateness time.Duration

	// Usual marks one of the user's habitual trains (config usual_train_nos).
	Usual bool
	// UnknownType marks a train kept under the include_and_flag policy, whose
	// electronic-ticket eligibility could not be established.
	UnknownType bool
}

// DelayMinutes returns the delay rounded to whole minutes, the unit TDX
// publishes and the unit the message displays.
func (c Candidate) DelayMinutes() int { return int(c.Delay / time.Minute) }

// LatenessMinutes returns the lateness in whole minutes.
func (c Candidate) LatenessMinutes() int { return int(c.Lateness / time.Minute) }
