package usecase

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// SettingsOp reads the current settings and returns the settings to persist
// (unchanged, if the op is a pure read) plus an arbitrary result for the
// caller.
type SettingsOp func(domain.SettingsList) (domain.SettingsList, any)

// settingsOutcome is what one request's round trip through the actor
// produced: either a result or the reason it could not be produced. Load and
// Save failures must reach the caller as an error rather than a nil-error
// result — a silently-empty result reads as "the op ran and returned
// nothing," not "your edit never touched disk."
type settingsOutcome struct {
	value any
	err   error
}

type settingsRequest struct {
	op     SettingsOp
	result chan settingsOutcome
}

// SettingsActor is the single goroutine allowed to call
// SettingsStore.Load/Save (§10.9). The notify loop and the Telegram command
// loop run concurrently and both need to touch settings.json — the notify
// loop reads it every minute, the command loop writes it whenever /setup or
// /manage completes a step — so without coordination a write landing between
// a read and its use would be a race.
//
// Routing every access through one actor goroutine makes "read the current
// list, decide, write the result back" atomic by construction: the actor
// processes one request at a time, so no two operations can interleave, and
// there is no mutex to remember to hold or a lock to forget to release.
type SettingsActor struct {
	store    SettingsStore
	log      *slog.Logger
	requests chan settingsRequest
}

// NewSettingsActor builds an actor. Run must be started in its own goroutine
// before Do is called from anywhere.
func NewSettingsActor(store SettingsStore, log *slog.Logger) *SettingsActor {
	return &SettingsActor{store: store, log: log, requests: make(chan settingsRequest)}
}

// Run processes requests until ctx is cancelled. Exactly one goroutine must
// run this for the lifetime of the process — see the SettingsActor doc
// comment for why a second entry point back into SettingsStore would defeat
// the whole point.
func (a *SettingsActor) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-a.requests:
			current, err := a.store.Load()
			if err != nil {
				// A request built on an unreadable file must not be allowed
				// to proceed: req.op ran against domain.SettingsList{} would
				// then get Saved over whatever is actually on disk, deleting
				// every existing Schedule. Rejecting the request instead
				// costs this one caller an error; starting from empty and
				// writing it back could cost every Schedule that exists.
				a.log.Error("settings load failed, rejecting request", "err", err)
				req.result <- settingsOutcome{err: fmt.Errorf("load settings: %w", err)}
				continue
			}
			updated, result := req.op(current)
			if err := a.store.Save(updated); err != nil {
				// A failed save must reach the caller as an error — the op's
				// result reflects what the caller asked for, but it did not
				// make it to disk, and the actor always reloads from disk on
				// the next request, so a silent "success" here would be a
				// permanently lost edit, not a deferred one.
				a.log.Error("settings save failed", "err", err)
				req.result <- settingsOutcome{err: fmt.Errorf("save settings: %w", err)}
				continue
			}
			req.result <- settingsOutcome{value: result}
		}
	}
}

// Do submits op to the actor and blocks for its result, or for ctx to be
// cancelled. Safe to call from any goroutine at any time — including from
// inside a long poll elsewhere, since the actor only ever blocks on file I/O.
func (a *SettingsActor) Do(ctx context.Context, op SettingsOp) (any, error) {
	req := settingsRequest{op: op, result: make(chan settingsOutcome, 1)}
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

// ActorStore adapts a SettingsActor to the plain SettingsStore interface
// Tick expects, so the notify loop reads settings through the same
// serialized path the command loop writes them through, without Tick itself
// needing to know an actor is involved.
type ActorStore struct {
	Actor *SettingsActor
	// Ctx bounds every request; it should be the daemon's shutdown context; a
	// stopped actor would otherwise leave a caller blocked forever.
	Ctx context.Context
}

// Load reads the current settings through the actor's serialized path.
func (a ActorStore) Load() (domain.SettingsList, error) {
	res, err := a.Actor.Do(a.Ctx, func(cur domain.SettingsList) (domain.SettingsList, any) {
		return cur, cur
	})
	if err != nil {
		return domain.SettingsList{}, err
	}
	return res.(domain.SettingsList), nil
}

// Save writes the given settings through the actor's serialized path.
func (a ActorStore) Save(list domain.SettingsList) error {
	_, err := a.Actor.Do(a.Ctx, func(domain.SettingsList) (domain.SettingsList, any) {
		return list, nil
	})
	return err
}
