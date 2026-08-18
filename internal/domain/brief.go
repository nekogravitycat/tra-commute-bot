package domain

import "time"

// Mode selects the message template (§9). It is derived, never set by hand, so
// the renderer and the tests agree on what "today is a late day" means.
type Mode int

const (
	// ModeNormal is the everyday brief: the recommendation gets the user in
	// on time (§9.1).
	ModeNormal Mode = iota
	// ModeLate means the best available train still misses the deadline, so
	// the compensation options move to the top (§9.2).
	ModeLate
	// ModeSevere is ModeLate past the severe threshold: the message escalates
	// and suggests contacting a manager, but never does so itself.
	ModeSevere
	// ModeNoService means the window contained no boardable train.
	ModeNoService
	// ModeDegraded means data collection failed and the brief is a warning
	// plus whatever could still be salvaged (§9.3).
	ModeDegraded
)

func (m Mode) String() string {
	switch m {
	case ModeLate:
		return "late"
	case ModeSevere:
		return "severe"
	case ModeNoService:
		return "no_service"
	case ModeDegraded:
		return "degraded"
	default:
		return "normal"
	}
}

// Route names the leg being planned, for the message header.
type Route struct {
	OriginName      string
	DestinationName string
}

// Degradation explains a partial or total data failure (§9.3).
type Degradation struct {
	// Stage is the step that failed, e.g. "timetable" or "live".
	Stage string
	// Detail is a short human-readable cause.
	Detail string
	// SchedulesUsable reports whether the timetable survived, in which case
	// the message can still list scheduled times marked as not delay-adjusted.
	SchedulesUsable bool
}

// Brief is the complete, render-ready result of one run. Everything the
// message needs is here; the renderer performs no business logic.
type Brief struct {
	Mode        Mode
	GeneratedAt time.Time
	Schedule    string // the schedule rule that fired, for the log
	Route       Route
	Params      Params

	Plan          Plan
	Compensations []Compensation
	Certificate   Certificate

	// LiveDataAvailable is false when the live board could not be fetched, in
	// which case every delay is an assumption and the message must say so.
	LiveDataAvailable bool
	// DataUpdatedAt is the live board's own timestamp.
	DataUpdatedAt time.Time

	// Degradation is set only in ModeDegraded.
	Degradation *Degradation
}

// BriefInput carries the settings BuildBrief needs beyond the plan itself.
type BriefInput struct {
	GeneratedAt       time.Time
	Schedule          string
	Route             Route
	Params            Params
	Plan              Plan
	LiveDataAvailable bool
	DataUpdatedAt     time.Time

	CertificateEnabled  bool
	CertificateMinDelay time.Duration

	CompensationEnabled bool
	MaxEarlyLeave       time.Duration
	SevereThreshold     time.Duration
}

// BuildBrief assembles the final brief, running the certificate and
// compensation searches only when they can actually say something.
func BuildBrief(in BriefInput) Brief {
	b := Brief{
		GeneratedAt:       in.GeneratedAt,
		Schedule:          in.Schedule,
		Route:             in.Route,
		Params:            in.Params,
		Plan:              in.Plan,
		LiveDataAvailable: in.LiveDataAvailable,
		DataUpdatedAt:     in.DataUpdatedAt,
	}

	rec := in.Plan.Recommended
	if rec == nil {
		b.Mode = ModeNoService
		return b
	}

	switch {
	case rec.Lateness == 0:
		b.Mode = ModeNormal
	case rec.Lateness > in.SevereThreshold:
		b.Mode = ModeSevere
	default:
		b.Mode = ModeLate
	}

	if rec.Lateness > 0 {
		if in.CompensationEnabled {
			b.Compensations = FindCompensations(in.Plan, in.Params, in.MaxEarlyLeave)
		}
		if in.CertificateEnabled {
			b.Certificate = FindCertificate(in.Plan, *rec, in.CertificateMinDelay)
		}
	}
	return b
}

// DegradedBrief builds the §9.3 fallback. Nothing may end silently: even a
// total failure produces a message, because a missing notification is
// indistinguishable from "no trains are delayed today".
func DegradedBrief(in BriefInput, d Degradation) Brief {
	return Brief{
		Mode:              ModeDegraded,
		GeneratedAt:       in.GeneratedAt,
		Schedule:          in.Schedule,
		Route:             in.Route,
		Params:            in.Params,
		Plan:              in.Plan,
		LiveDataAvailable: false,
		Degradation:       &d,
	}
}

// BestCompensation returns the option the message should lead with, if any.
func (b Brief) BestCompensation() *Compensation {
	if len(b.Compensations) == 0 {
		return nil
	}
	return &b.Compensations[0]
}
