package usecase

import (
	"context"
	"log/slog"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// Tick is one wake-up of the every-minute timer (§10.3). It applies the guard,
// and only when the guard says so does it run the brief and record the outcome.
//
// Almost every invocation ends after reading the config and the state file,
// without a single network call, which is what makes the once-a-minute design
// affordable.
type Tick struct {
	Clock  Clock
	State  StateStore
	Brief  *Brief
	Log    *slog.Logger
	Rules  domain.Scheduling
	DryRun bool
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

	decision := domain.DecideTick(now, t.Rules, state)
	out := TickResult{Decision: decision}

	switch decision.Action {
	case domain.TickNone:
		t.Log.Debug("tick idle", "reason", decision.Reason)
		return out, nil

	case domain.TickGiveUp:
		t.Log.Error("giving up on schedule", "schedule", decision.Schedule.Name, "reason", decision.Reason)
		res, err := t.Brief.RunDegraded(ctx, decision.FiredAt, decision.Schedule.Name, decision.Reason)
		out.Ran, out.Result = true, res
		t.recordGaveUp(&state, decision, now)
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

	res, err := t.Brief.Run(ctx, decision.FiredAt, decision.Schedule.Name)
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
