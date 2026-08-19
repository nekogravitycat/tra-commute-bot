package usecase

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// Op reads a document's current value and returns the value to persist
// (unchanged, if the op is a pure read) plus an arbitrary result for the
// caller.
type Op[T any] func(T) (T, any)

// outcome is what one request's round trip through an actor produced: either
// a result or the reason it could not be produced. Load and Save failures
// must reach the caller as an error rather than a nil-error result — a
// silently-empty result reads as "the op ran and returned nothing," not
// "your edit never touched disk."
type outcome struct {
	value any
	err   error
}

type request[T any] struct {
	op     Op[T]
	result chan outcome
}

// Actor is the single goroutine allowed to call one Store's Load and Save
// (§10.9). Both of this program's documents need one, for the same reason:
//
//   - settings.json is read by the notify loop every minute and written by
//     the Telegram command loop whenever /setup or /manage completes a step;
//   - state.json is written by the notify loop on every attempt, success and
//     give-up, and by a /manage deletion clearing a Schedule's guard history
//     (§10.6).
//
// Both pairs run in different goroutines, so without coordination a write
// landing between a read and its use would be a race.
//
// Routing every access through one actor goroutine makes "read the current
// value, decide, write the result back" atomic by construction: the actor
// processes one request at a time, so no two operations can interleave, and
// there is no mutex to remember to hold or a lock to forget to release.
//
// SettingsActor and StateActor name the two instantiations.
type Actor[T any] struct {
	store Store[T]
	// name labels this actor's document in its log lines and wrapped errors
	// ("settings", "state"), so a failure says which file it was about.
	name     string
	log      *slog.Logger
	requests chan request[T]
}

// SettingsActor serializes access to settings.json (§10.9).
type SettingsActor = Actor[domain.SettingsList]

// StateActor serializes access to state.json, mirroring SettingsActor for
// the identical reason.
type StateActor = Actor[domain.TickState]

// NewSettingsActor builds the actor for settings.json. Run must be started in
// its own goroutine before Do is called from anywhere.
func NewSettingsActor(store SettingsStore, log *slog.Logger) *SettingsActor {
	return newActor("settings", store, log)
}

// NewStateActor builds the actor for state.json. Run must be started in its
// own goroutine before Do is called from anywhere.
func NewStateActor(store StateStore, log *slog.Logger) *StateActor {
	return newActor("state", store, log)
}

func newActor[T any](name string, store Store[T], log *slog.Logger) *Actor[T] {
	return &Actor[T]{store: store, name: name, log: log, requests: make(chan request[T])}
}

// Run processes requests until ctx is cancelled. Exactly one goroutine must
// run this for the lifetime of the process — see the Actor doc comment for
// why a second entry point back into the Store would defeat the whole point.
func (a *Actor[T]) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-a.requests:
			req.result <- a.serve(req.op)
		}
	}
}

// serve is one request's load-decide-save round trip, run on the actor
// goroutine and nowhere else.
func (a *Actor[T]) serve(op Op[T]) outcome {
	current, err := a.store.Load()
	if err != nil {
		// A request built on an unreadable file must not be allowed to
		// proceed: op running against the zero value would then get Saved
		// over whatever is actually on disk, deleting every existing Schedule
		// (or every Schedule's guard history). Rejecting the request instead
		// costs this one caller an error; starting from empty and writing it
		// back could cost the whole file.
		a.log.Error(a.name+" load failed, rejecting request", "err", err)
		return outcome{err: fmt.Errorf("load %s: %w", a.name, err)}
	}
	updated, result := op(current)
	if err := a.store.Save(updated); err != nil {
		// A failed save must reach the caller as an error — the op's result
		// reflects what the caller asked for, but it did not make it to disk,
		// and the actor always reloads from disk on the next request, so a
		// silent "success" here would be a permanently lost edit, not a
		// deferred one.
		a.log.Error(a.name+" save failed", "err", err)
		return outcome{err: fmt.Errorf("save %s: %w", a.name, err)}
	}
	return outcome{value: result}
}

// Do submits op to the actor and blocks for its result, or for ctx to be
// cancelled. Safe to call from any goroutine at any time — including from
// inside a long poll elsewhere, since the actor only ever blocks on file I/O.
func (a *Actor[T]) Do(ctx context.Context, op Op[T]) (any, error) {
	req := request[T]{op: op, result: make(chan outcome, 1)}
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

// ActorStore adapts an Actor back to the plain Store interface Tick and the
// command package expect, so the notify loop reads through the same
// serialized path the command loop writes through, without either side
// needing to know an actor is involved.
type ActorStore[T any] struct {
	Actor *Actor[T]
	// Ctx bounds every request; it should be the daemon's shutdown context. A
	// stopped actor would otherwise leave a caller blocked forever.
	Ctx context.Context
}

// Load reads the current value through the actor's serialized path.
func (a ActorStore[T]) Load() (T, error) {
	res, err := a.Actor.Do(a.Ctx, func(cur T) (T, any) { return cur, cur })
	if err != nil {
		var zero T
		return zero, err
	}
	return res.(T), nil
}

// Save writes value through the actor's serialized path.
func (a ActorStore[T]) Save(value T) error {
	_, err := a.Actor.Do(a.Ctx, func(T) (T, any) { return value, nil })
	return err
}
