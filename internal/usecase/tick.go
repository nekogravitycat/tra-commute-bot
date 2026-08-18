package usecase

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// Tick is one wake-up of the every-minute notify loop (§5.1, §10.8). It
// applies the guard to every configured Schedule, and only for the ones the
// guard says are due does it run the brief and record the outcome.
//
// Almost every invocation ends after reading the state file and the live
// settings, without a single network call — the once-a-minute design stays
// cheap even though the process itself now stays up all day (§4.2) rather
// than exiting between wake-ups.
type Tick struct {
	Clock    Clock
	State    StateStore
	Settings SettingsStore
	Brief    *Brief
	Log      *slog.Logger

	// SkipDates, ExtraDates, Tolerance and RetryWindow are the guard
	// parameters that stay in config.yaml: they change rarely, unlike each
	// Schedule's own weekdays and fire time, which the user edits live via
	// /setup and /manage and which are read out of Settings on every tick.
	SkipDates   []string
	ExtraDates  []string // manual make-up workdays, admin-managed
	Tolerance   time.Duration
	RetryWindow time.Duration

	DryRun bool
}

// scheduling merges the config-file guard parameters with every live
// Schedule the user has configured, so a change to either takes effect on
// the very next tick. ExtraDates is a global make-up-workday calendar (§8)
// shared by all Schedules, so it is appended to each one individually.
func (t *Tick) scheduling(list domain.SettingsList) domain.Scheduling {
	schedules := make([]domain.Schedule, 0, len(list.Schedules))
	for _, s := range list.Schedules {
		sch := s.Schedule()
		sch.Dates = append(append([]string{}, sch.Dates...), t.ExtraDates...)
		schedules = append(schedules, sch)
	}
	return domain.Scheduling{
		Schedules:   schedules,
		SkipDates:   t.SkipDates,
		Tolerance:   t.Tolerance,
		RetryWindow: t.RetryWindow,
	}
}

// TickOutcome is what happened for one Schedule that the guard found due.
type TickOutcome struct {
	Decision domain.TickDecision
	Result   Result
}

// TickResult reports what the wake-up did, across every Schedule the guard
// found due this minute — ordinarily zero or one, but two Schedules sharing a
// notify time both fire in the same tick (§10.8).
type TickResult struct {
	Outcomes []TickOutcome
}

// Ran reports whether any Schedule actually produced a brief this tick.
func (r TickResult) Ran() bool { return len(r.Outcomes) > 0 }

// Run evaluates the guard for every configured Schedule and, for each one
// that is due, produces the brief.
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

	list, err := t.Settings.Load()
	if err != nil {
		t.Log.Warn("settings unreadable, continuing with no schedules", "err", err)
		list = domain.SettingsList{}
	}

	decisions := domain.DecideTicks(now, t.scheduling(list), state)

	var out TickResult
	var errs []error
	for _, d := range decisions {
		switch d.Action {
		case domain.TickNone:
			t.Log.Debug("tick idle", "reason", d.Reason)
			continue

		case domain.TickGiveUp:
			t.Log.Error("giving up on schedule", "schedule", d.Schedule.Name, "reason", d.Reason)
			trip, _ := list.Find(d.Schedule.Name)
			res, err := t.Brief.RunDegraded(ctx, d.FiredAt, d.Schedule.Name, d.Reason, trip)
			out.Outcomes = append(out.Outcomes, TickOutcome{Decision: d, Result: res})
			t.recordGaveUp(&state, d, now)
			if err != nil {
				errs = append(errs, err)
			}
			continue
		}

		trip, ok := list.Find(d.Schedule.Name)
		if !ok {
			// The guard matched a Schedule that vanished from settings.json
			// between Load and here — only possible if /manage deleted it
			// mid-tick. Nothing to run; the next tick will not see it either.
			t.Log.Warn("schedule disappeared before it could run", "schedule", d.Schedule.Name)
			continue
		}

		t.Log.Info("running brief",
			"schedule", d.Schedule.Name,
			"fired_at", d.FiredAt,
			"retry", d.Retry)

		// The attempt is recorded before the run, not after. If the process
		// dies mid-flight the next tick must still see that an attempt
		// happened, otherwise the retry window never starts counting.
		t.recordAttempt(&state, d, now)

		res, err := t.Brief.Run(ctx, d.FiredAt, d.Schedule.Name, trip)
		out.Outcomes = append(out.Outcomes, TickOutcome{Decision: d, Result: res})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		t.recordSuccess(&state, d, now)
	}

	return out, errors.Join(errs...)
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
