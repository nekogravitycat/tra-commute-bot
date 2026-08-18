package domain

import "time"

// Params are the user's constraints for one run, resolved to absolute instants.
// Ready and Deadline are absolute rather than clock times so that a schedule
// running near midnight, or a deadline that falls on the following day, needs
// no special handling anywhere downstream.
type Params struct {
	// Ready is T_ready: the earliest the user can be standing at the origin
	// station (the schedule's fire time plus ready_lead_minutes).
	Ready time.Time
	// Deadline is the clock-in deadline.
	Deadline time.Time
	// LastMile is the time from arriving at the destination station to
	// clocking in.
	LastMile time.Duration
	// BoardBuffer is the slack needed between reaching the platform and the
	// train pulling away.
	BoardBuffer time.Duration
	// RiskMargin is how much slack beyond BoardBuffer a departure needs
	// before it counts as comfortably catchable rather than risky.
	RiskMargin time.Duration
}

// EarliestBoarding is the first instant a departure is theoretically catchable.
func (p Params) EarliestBoarding() time.Time { return p.Ready.Add(p.BoardBuffer) }

// SafeBoarding is the first instant a departure is catchable with enough slack
// that a shrinking delay will not strand the user on the platform.
func (p Params) SafeBoarding() time.Time { return p.EarliestBoarding().Add(p.RiskMargin) }

// ClockInFor returns when the user badges in if they arrive at the destination
// station at arr.
func (p Params) ClockInFor(arr time.Time) time.Time { return arr.Add(p.LastMile) }

// LatenessFor returns how late the user is if they arrive at the destination
// station at arr, floored at zero.
func (p Params) LatenessFor(arr time.Time) time.Duration {
	d := p.ClockInFor(arr).Sub(p.Deadline)
	if d < 0 {
		return 0
	}
	return d
}

// SlackFor returns the spare time before the deadline, floored at zero. It is
// the mirror of LatenessFor and exists so the renderer never has to negate a
// duration to print "餘裕 8 分".
func (p Params) SlackFor(arr time.Time) time.Duration {
	d := p.Deadline.Sub(p.ClockInFor(arr))
	if d < 0 {
		return 0
	}
	return d
}
