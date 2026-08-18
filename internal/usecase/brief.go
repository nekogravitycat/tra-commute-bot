package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// BriefSettings are the parts of the configuration this use case needs, already
// validated and converted into domain types by internal/config.
type BriefSettings struct {
	Route      domain.Route
	OriginID   string
	DestID     string
	ReadyLead  time.Duration
	Deadline   domain.TimeOfDay
	LastMile   time.Duration
	Board      time.Duration
	RiskMargin time.Duration

	Window        domain.Window
	Filter        domain.TypeFilter
	UsualTrainNos []string

	CertificateEnabled  bool
	CertificateMinDelay time.Duration

	CompensationEnabled bool
	MaxEarlyLeave       time.Duration
	SevereThreshold     time.Duration
}

// Brief produces and delivers one commute brief.
type Brief struct {
	Timetable TimetableSource
	Delays    DelaySource
	Renderer  Renderer
	Notifier  Notifier
	Archiver  Archiver // optional
	Log       *slog.Logger

	Settings BriefSettings
	// SendRetries is how many extra attempts a failed delivery gets.
	SendRetries int
	// SendBackoff is the wait before the first delivery retry; it doubles.
	SendBackoff time.Duration
	// Sleep is injected so delivery retries do not make tests slow.
	Sleep func(time.Duration)
}

// Result reports what one run produced, for the caller's logging and exit code.
type Result struct {
	Brief   domain.Brief
	Message Message
	// Delivered reports whether the notification actually went out. Only a
	// delivered notification counts as success: a brief computed but never
	// sent is exactly the silent failure the system exists to prevent.
	Delivered bool
}

// Run executes steps 3 through 8 of §5.1 for the schedule that fired at
// firedAt.
//
// Every data-collection failure is caught and turned into a degraded brief
// rather than an early return. The only error Run reports is a failure to
// deliver anything at all.
func (b *Brief) Run(ctx context.Context, firedAt time.Time, scheduleName string) (Result, error) {
	params := b.params(firedAt)
	in := domain.BriefInput{
		GeneratedAt:         firedAt,
		Schedule:            scheduleName,
		Route:               b.Settings.Route,
		Params:              params,
		CertificateEnabled:  b.Settings.CertificateEnabled,
		CertificateMinDelay: b.Settings.CertificateMinDelay,
		CompensationEnabled: b.Settings.CompensationEnabled,
		MaxEarlyLeave:       b.Settings.MaxEarlyLeave,
		SevereThreshold:     b.Settings.SevereThreshold,
	}

	timetable, err := b.Timetable.DailyODTimetable(ctx, b.Settings.OriginID, b.Settings.DestID, firedAt)
	if err != nil {
		b.Log.Error("timetable fetch failed", "err", err)
		return b.deliver(ctx, domain.DegradedBrief(in, domain.Degradation{
			Stage:  "timetable",
			Detail: err.Error(),
		}))
	}

	// A live-board failure is survivable in a way a timetable failure is not:
	// the scheduled times are still worth sending, as long as the message is
	// explicit that no delays have been applied.
	var delays DelaySnapshot
	live := true
	if snap, err := b.Delays.LiveDelays(ctx); err != nil {
		b.Log.Error("live board fetch failed", "err", err)
		live = false
		delays = DelaySnapshot{ByTrainNo: map[string]int{}}
	} else {
		delays = snap
	}

	plan := domain.BuildPlan(domain.PlanInput{
		Services:      timetable.Services,
		Delays:        delays.ByTrainNo,
		Params:        params,
		Window:        b.Settings.Window,
		Filter:        b.Settings.Filter,
		UsualTrainNos: b.Settings.UsualTrainNos,
	})
	if len(plan.UnknownTypes) > 0 {
		// Logged so the unrecognised names can be folded into the config
		// rather than surprising the reader again tomorrow.
		b.Log.Warn("unrecognised train types kept as candidates", "types", plan.UnknownTypes)
	}

	in.Plan = plan
	in.LiveDataAvailable = live
	in.DataUpdatedAt = delays.UpdatedAt

	if !live {
		return b.deliver(ctx, domain.DegradedBrief(in, domain.Degradation{
			Stage:           "live",
			Detail:          "無法取得即時誤點資料",
			SchedulesUsable: true,
		}))
	}
	return b.deliver(ctx, domain.BuildBrief(in))
}

// RunDegraded sends the §9.3 warning for a run that never got as far as
// fetching anything — the give-up path after the retry window expires.
func (b *Brief) RunDegraded(ctx context.Context, firedAt time.Time, scheduleName, detail string) (Result, error) {
	in := domain.BriefInput{
		GeneratedAt: firedAt,
		Schedule:    scheduleName,
		Route:       b.Settings.Route,
		Params:      b.params(firedAt),
	}
	return b.deliver(ctx, domain.DegradedBrief(in, domain.Degradation{Stage: "run", Detail: detail}))
}

func (b *Brief) params(firedAt time.Time) domain.Params {
	deadline := b.Settings.Deadline.On(firedAt)
	ready := firedAt.Add(b.Settings.ReadyLead)
	// A schedule late enough in the day pushes T_ready past a deadline
	// expressed as a wall-clock time; the deadline then belongs to tomorrow.
	// v0.1 never configures such a schedule, but the guard costs one branch
	// and removes a whole class of nonsense results.
	if deadline.Before(ready) {
		deadline = deadline.AddDate(0, 0, 1)
	}
	return domain.Params{
		Ready:       ready,
		Deadline:    deadline,
		LastMile:    b.Settings.LastMile,
		BoardBuffer: b.Settings.Board,
		RiskMargin:  b.Settings.RiskMargin,
	}
}

func (b *Brief) deliver(ctx context.Context, br domain.Brief) (Result, error) {
	msg := b.Renderer.Render(br)
	res := Result{Brief: br, Message: msg}

	err := b.send(ctx, msg)
	if err != nil {
		return res, fmt.Errorf("deliver brief: %w", err)
	}
	res.Delivered = true
	return res, nil
}

func (b *Brief) send(ctx context.Context, m Message) error {
	sleep := b.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	backoff := b.SendBackoff
	if backoff <= 0 {
		backoff = time.Second
	}

	var errs []error
	for attempt := 0; attempt <= b.SendRetries; attempt++ {
		if attempt > 0 {
			sleep(backoff)
			backoff *= 2
		}
		if err := b.Notifier.Send(ctx, m); err != nil {
			errs = append(errs, err)
			b.Log.Warn("notification send failed", "attempt", attempt+1, "err", err)
			continue
		}
		return nil
	}
	return errors.Join(errs...)
}
