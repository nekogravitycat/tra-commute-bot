package usecase

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// TestSettingsActorSerializesConcurrentAppends is the whole point of §10.9:
// many goroutines appending a uniquely named schedule concurrently must never
// lose one to a lost update, the way a bare read-modify-write without a lock
// would.
func TestSettingsActorSerializesConcurrentAppends(t *testing.T) {
	store := &fakeSettings{settings: &domain.SettingsList{}}
	actor := NewSettingsActor(store, quietLogger())

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
			_, err := actor.Do(ctx, func(cur domain.SettingsList) (domain.SettingsList, any) {
				return cur.Upsert(domain.Settings{Name: name}), nil
			})
			if err != nil {
				t.Errorf("Do: %v", err)
			}
		}(i)
	}
	wg.Wait()

	res, err := actor.Do(ctx, func(cur domain.SettingsList) (domain.SettingsList, any) { return cur, cur })
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	got := res.(domain.SettingsList)
	if len(got.Schedules) != n {
		t.Errorf("got %d schedules, want %d — a concurrent write was lost", len(got.Schedules), n)
	}
}

// TestSettingsActorReadYourWrites checks a write followed immediately by a
// read (as /manage's confirmation message needs) sees the write, not a
// stale value racing in from disk.
func TestSettingsActorReadYourWrites(t *testing.T) {
	store := &fakeSettings{}
	actor := NewSettingsActor(store, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go actor.Run(ctx)

	_, err := actor.Do(ctx, func(cur domain.SettingsList) (domain.SettingsList, any) {
		return cur.Upsert(domain.Settings{Name: "上班通勤"}), nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	res, err := actor.Do(ctx, func(cur domain.SettingsList) (domain.SettingsList, any) { return cur, cur })
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if _, ok := res.(domain.SettingsList).Find("上班通勤"); !ok {
		t.Error("expected to read back the schedule just written")
	}
}

// TestActorStoreImplementsSettingsStore checks the Tick-facing adapter reads
// and writes through the actor rather than bypassing it — the §10.9
// invariant that exactly one goroutine ever calls the underlying store.
func TestActorStoreImplementsSettingsStore(t *testing.T) {
	store := &fakeSettings{}
	actor := NewSettingsActor(store, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go actor.Run(ctx)

	as := ActorStore{Actor: actor, Ctx: ctx}
	if err := as.Save(domain.SettingsList{Schedules: []domain.Settings{{Name: "上班通勤"}}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := as.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := got.Find("上班通勤"); !ok {
		t.Error("expected ActorStore.Load to see ActorStore.Save's write")
	}
}

// TestSettingsActorDoRespectsCancellation checks a caller is not left
// blocked forever if the actor has already stopped — the failure mode a
// stray direct SettingsStore access would otherwise hide until deployment.
func TestSettingsActorDoRespectsCancellation(t *testing.T) {
	store := &fakeSettings{}
	actor := NewSettingsActor(store, quietLogger())
	// No Run goroutine started: every request must time out via ctx, not hang.

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := actor.Do(ctx, func(cur domain.SettingsList) (domain.SettingsList, any) { return cur, nil })
	if err == nil {
		t.Error("expected Do to report the cancelled context rather than hang")
	}
}
