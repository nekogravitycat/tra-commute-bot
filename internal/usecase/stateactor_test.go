package usecase

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// TestStateActorSerializesConcurrentWrites is M-1: the notify loop recording
// a tick attempt and a /manage deletion clearing another Schedule's history
// run in different goroutines and both touch state.json — many goroutines
// each setting a uniquely keyed LastSuccess entry concurrently must never
// lose one to a lost update, the same guarantee SettingsActor gives §10.9.
func TestStateActorSerializesConcurrentWrites(t *testing.T) {
	store := &fakeState{}
	actor := NewStateActor(store, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go actor.Run(ctx)

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "schedule-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
			_, err := actor.Do(ctx, func(cur domain.TickState) (domain.TickState, any) {
				if cur.LastSuccess == nil {
					cur.LastSuccess = map[string]string{}
				}
				cur.LastSuccess[name] = "2026-08-19"
				return cur, nil
			})
			if err != nil {
				t.Errorf("Do: %v", err)
			}
		}(i)
	}
	wg.Wait()

	res, err := actor.Do(ctx, func(cur domain.TickState) (domain.TickState, any) { return cur, cur })
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	got := res.(domain.TickState)
	if len(got.LastSuccess) != n {
		t.Errorf("got %d entries, want %d — a concurrent write was lost", len(got.LastSuccess), n)
	}
}

// TestStateActorRejectsWriteOnUnreadableFile mirrors the H-5 fix for
// settings: an op must not run against an empty base and get written back
// when state.json cannot be read, or every Schedule's guard history would be
// wiped by the next write.
func TestStateActorRejectsWriteOnUnreadableFile(t *testing.T) {
	store := &fakeState{
		state:   domain.TickState{LastSuccess: map[string]string{"existing": "2026-08-18"}},
		loadErr: errBoom,
	}
	actor := NewStateActor(store, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go actor.Run(ctx)

	_, err := actor.Do(ctx, func(cur domain.TickState) (domain.TickState, any) {
		cur.LastSuccess["new"] = "2026-08-19"
		return cur, nil
	})
	if err == nil {
		t.Fatal("expected Do to report the load failure rather than silently proceeding")
	}
	if store.saves != 0 {
		t.Errorf("got %d saves, want 0 — a request built on an unreadable file must never be written back", store.saves)
	}
}

// TestStateActorReportsSaveFailure mirrors the H-4 fix for settings: a
// failed Save must reach the caller as an error, not a silent success.
func TestStateActorReportsSaveFailure(t *testing.T) {
	store := &fakeState{saveErr: errBoom}
	actor := NewStateActor(store, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go actor.Run(ctx)

	_, err := actor.Do(ctx, func(cur domain.TickState) (domain.TickState, any) { return cur, nil })
	if err == nil {
		t.Fatal("expected Do to report the save failure rather than silently succeeding")
	}
}

// TestStateActorStoreReadYourWrites checks StateActorStore.Save followed by
// StateActorStore.Load sees the write, matching ActorStore's guarantee for
// settings.
func TestStateActorStoreReadYourWrites(t *testing.T) {
	store := &fakeState{}
	actor := NewStateActor(store, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go actor.Run(ctx)

	as := StateActorStore{Actor: actor, Ctx: ctx}
	if err := as.Save(domain.TickState{LastSuccess: map[string]string{"上班通勤": "2026-08-19"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := as.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LastSuccess["上班通勤"] != "2026-08-19" {
		t.Error("expected StateActorStore.Load to see StateActorStore.Save's write")
	}
}

// TestStateActorDoRespectsCancellation checks a caller is not left blocked
// forever if the actor has already stopped.
func TestStateActorDoRespectsCancellation(t *testing.T) {
	store := &fakeState{}
	actor := NewStateActor(store, quietLogger())
	// No Run goroutine started: every request must time out via ctx.

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := actor.Do(ctx, func(cur domain.TickState) (domain.TickState, any) { return cur, nil })
	if err == nil {
		t.Error("expected Do to report the cancelled context rather than hang")
	}
}
