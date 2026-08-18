package usecase

import (
	"context"
	"log/slog"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// Tick is one wake-up of the every-minute loop (§10.3). It applies the guard,
// and only when the guard says so does it run the brief and record the outcome.
//
// Almost every invocation ends after reading the state file and the live
// settings, without a single network call, which is what makes the
// once-a-minute design affordable even though the process itself now stays
// up all day rather than exiting between wake-ups.
type Tick struct {
	Clock    Clock
	State    StateStore
	Settings SettingsStore
	Brief    *Brief
	Log      *slog.Logger

	// SkipDates, ExtraDates, Tolerance and RetryWindow are the guard
	// parameters that stay in config.yaml: they change rarely, unlike the
	// schedule's own weekdays and fire time, which the user edits live via
	// /schedule and are read out of Settings on every tick.
	SkipDates   []string
	ExtraDates  []string // manual make-up workdays, admin-managed
	Tolerance   time.Duration
	RetryWindow time.Duration

	DryRun bool
}

// scheduling merges the config-file guard parameters with the live schedule
// the user set via /schedule, so a change to either takes effect on the very
// next tick.
func (t *Tick) scheduling(s domain.Settings) domain.Scheduling {
	sch := s.Schedule()
	sch.Dates = append(append([]string{}, sch.Dates...), t.ExtraDates...)
	return domain.Scheduling{
		Schedules:   []domain.Schedule{sch},
		SkipDates:   t.SkipDates,
		Tolerance:   t.Tolerance,
		RetryWindow: t.RetryWindow,
	}
}

// TickResult reports what the wake-up did.
type TickResult struct {
	Decision domain.TickDecision
	Ran      bool
	Result   Result
}

// Run evaluates the guard and, if due, produces the brief.
func (t *Tick) Run(ctx context.Context) (TickResult, error) {
	now := t.Clock.Now()

	state, err := t.State.Load()
	if err != nil {
		// A corrupt or unreadable state file must not become a permanent
		// silence. Starting from empty state risks one duplicate message,
		// which is strictly better than no message at all.
		t.Log.Warn("state unreadable, continuing with empty state", "err", err)
		state = domain.TickState{}
	}

	trip, err := t.Settings.Load()
	if err != nil {
		t.Log.Warn("settings unreadable, continuing with empty settings", "err", err)
		trip = domain.Settings{}
	}

	decision := domain.DecideTick(now, t.scheduling(trip), state)
	out := TickResult{Decision: decision}

	switch decision.Action {
	case domain.TickNone:
		t.Log.Debug("tick idle", "reason", decision.Reason)
		return out, nil

	case domain.TickGiveUp:
		t.Log.Error("giving up on schedule", "schedule", decision.Schedule.Name, "reason", decision.Reason)
		res, err := t.Brief.RunDegraded(ctx, decision.FiredAt, decision.Schedule.Name, decision.Reason, trip)
		out.Ran, out.Result = true, res
		t.recordGaveUp(&state, decision, now)
		return out, err
	}

	if complete, missing := trip.Complete(); !complete {
		// The schedule is configured and due, but the user has not finished
		// telling the bot the rest (most often right after a fresh
		// install). One reminder is enough — recording it as a success
		// keeps the retry loop from repeating it every minute for the rest
		// of the tolerance/retry window.
		t.Log.Warn("settings incomplete, sending reminder instead of a brief", "missing", missing)
		res, err := t.Brief.RunIncomplete(ctx, decision.FiredAt, decision.Schedule.Name, missing)
		out.Ran, out.Result = true, res
		t.recordSuccess(&state, decision, now)
		return out, err
	}

	t.Log.Info("running brief",
		"schedule", decision.Schedule.Name,
		"fired_at", decision.FiredAt,
		"retry", decision.Retry)

	// The attempt is recorded before the run, not after. If the process dies
	// mid-flight the next tick must still see that an attempt happened,
	// otherwise the retry window never starts counting.
	t.recordAttempt(&state, decision, now)

	res, err := t.Brief.Run(ctx, decision.FiredAt, decision.Schedule.Name, trip)
	out.Ran, out.Result = true, res
	if err != nil {
		return out, err
	}

	t.recordSuccess(&state, decision, now)
	return out, nil
}

func (t *Tick) recordAttempt(state *domain.TickState, d domain.TickDecision, now time.Time) {
	if t.DryRun {
		return
	}
	if state.Attempts == nil {
		state.Attempts = map[string]domain.Attempt{}
	}
	a := state.AttemptOn(d.Schedule.Name, now)
	a.Date = domain.DateKeyOf(now)
	a.Count++
	a.LastAt = now
	state.Attempts[d.Schedule.Name] = a
	t.save(*state)
}

func (t *Tick) recordGaveUp(state *domain.TickState, d domain.TickDecision, now time.Time) {
	if t.DryRun {
		return
	}
	if state.Attempts == nil {
		state.Attempts = map[string]domain.Attempt{}
	}
	a := state.AttemptOn(d.Schedule.Name, now)
	a.Date = domain.DateKeyOf(now)
	a.GaveUp = true
	state.Attempts[d.Schedule.Name] = a
	t.save(*state)
}

func (t *Tick) recordSuccess(state *domain.TickState, d domain.TickDecision, now time.Time) {
	if t.DryRun {
		return
	}
	if state.LastSuccess == nil {
		state.LastSuccess = map[string]string{}
	}
	state.LastSuccess[d.Schedule.Name] = domain.DateKeyOf(now)
	t.save(*state)
}

func (t *Tick) save(state domain.TickState) {
	if err := t.State.Save(state); err != nil {
		// Losing the guard state costs at most a duplicate message tomorrow;
		// it is never a reason to abandon a brief that already went out.
		t.Log.Error("state save failed", "err", err)
	}
}
