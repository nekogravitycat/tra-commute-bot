package usecase

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// StateOp reads the current tick guard state and returns the state to
// persist (unchanged, if the op is a pure read) plus an arbitrary result for
// the caller.
type StateOp func(domain.TickState) (domain.TickState, any)

// stateOutcome mirrors settingsOutcome: either a result or the reason it
// could not be produced.
type stateOutcome struct {
	value any
	err   error
}

type stateRequest struct {
	op     StateOp
	result chan stateOutcome
}

// StateActor is the single goroutine allowed to call StateStore.Load/Save
// once the long-running process is up. The notify loop records an
// attempt/success/give-up on every tick, and a /manage deletion clears a
// Schedule's guard history (§10.6) — both run in different goroutines and
// both touch state.json, so without coordination one write can land between
// the other's read and its own write. Mirrors SettingsActor (§10.9) for the
// identical reason.
type StateActor struct {
	store    StateStore
	log      *slog.Logger
	requests chan stateRequest
}

// NewStateActor builds an actor. Run must be started in its own goroutine
// before Do is called from anywhere.
func NewStateActor(store StateStore, log *slog.Logger) *StateActor {
	return &StateActor{store: store, log: log, requests: make(chan stateRequest)}
}

// Run processes requests until ctx is cancelled. Exactly one goroutine must
// run this for the lifetime of the process.
func (a *StateActor) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-a.requests:
			current, err := a.store.Load()
			if err != nil {
				// As with SettingsActor: a request built on an unreadable file
				// must not proceed and get written back over whatever is
				// actually on disk.
				a.log.Error("state load failed, rejecting request", "err", err)
				req.result <- stateOutcome{err: fmt.Errorf("load state: %w", err)}
				continue
			}
			updated, result := req.op(current)
			if err := a.store.Save(updated); err != nil {
				a.log.Error("state save failed", "err", err)
				req.result <- stateOutcome{err: fmt.Errorf("save state: %w", err)}
				continue
			}
			req.result <- stateOutcome{value: result}
		}
	}
}

// Do submits op to the actor and blocks for its result, or for ctx to be
// cancelled.
func (a *StateActor) Do(ctx context.Context, op StateOp) (any, error) {
	req := stateRequest{op: op, result: make(chan stateOutcome, 1)}
	select {
	case a.requests <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case res := <-req.result:
		return res.value, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// StateActorStore adapts a StateActor to the plain StateStore interface Tick
// and the command package expect, so every access goes through the same
// serialized path without the caller needing to know an actor is involved.
type StateActorStore struct {
	Actor *StateActor
	// Ctx bounds every request; it should be the daemon's shutdown context.
	Ctx context.Context
}

// Load reads the current state through the actor's serialized path.
func (a StateActorStore) Load() (domain.TickState, error) {
	res, err := a.Actor.Do(a.Ctx, func(cur domain.TickState) (domain.TickState, any) {
		return cur, cur
	})
	if err != nil {
		return domain.TickState{}, err
	}
	return res.(domain.TickState), nil
}

// Save writes the given state through the actor's serialized path.
func (a StateActorStore) Save(st domain.TickState) error {
	_, err := a.Actor.Do(a.Ctx, func(domain.TickState) (domain.TickState, any) {
		return st, nil
	})
	return err
}
