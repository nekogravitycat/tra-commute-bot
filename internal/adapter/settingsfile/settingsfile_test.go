package settingsfile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := New(path)

	want := domain.Settings{
		ScheduleWeekdays: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
		ScheduleAt:       domain.TimeOfDay{Hour: 7, Minute: 50},
		ReadyAt:          domain.TimeOfDay{Hour: 8, Minute: 20},
		DeadlineAt:       domain.TimeOfDay{Hour: 9, Minute: 10},
		MaxEarlyLeave:    15 * time.Minute,
		OriginID:         "1080",
		OriginName:       "桃園",
		DestinationID:    "1000",
		DestinationName:  "臺北",
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
	if complete, missing := got.Complete(); !complete {
		t.Errorf("settings incomplete after round trip, missing %v", missing)
	}
}

// TestLoadMissingFile checks a fresh install starts cleanly, with an
// incomplete Settings{} rather than an error.
func TestLoadMissingFile(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "absent.json"))

	got, err := s.Load()
	if err != nil {
		t.Fatalf("a missing settings file should not be an error: %v", err)
	}
	if complete, missing := got.Complete(); complete {
		t.Errorf("fresh settings should be incomplete, got complete")
	} else if len(missing) == 0 {
		t.Error("expected missing fields to be named")
	}
}

func TestLoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := New(path).Load(); err == nil {
		t.Error("expected a decode error for a corrupt settings file")
	}
}

// TestUnsetTimesDoNotBecomeMidnight checks the zero TimeOfDay round-trips as
// absent rather than as a literal 00:00 — otherwise a half-finished setup
// would look complete.
func TestUnsetTimesDoNotBecomeMidnight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := New(path)

	if err := s.Save(domain.Settings{OriginID: "1080", DestinationID: "1000"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ReadyAt != (domain.TimeOfDay{}) || got.DeadlineAt != (domain.TimeOfDay{}) {
		t.Errorf("ready/deadline = %+v/%+v, want both unset", got.ReadyAt, got.DeadlineAt)
	}
	if complete, _ := got.Complete(); complete {
		t.Error("settings with no ready/deadline/schedule should not be complete")
	}
}

// TestSaveIsAtomic checks no partial file is left behind after several saves.
func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	s := New(path)

	for i := 0; i < 3; i++ {
		if err := s.Save(domain.Settings{OriginID: "1080"}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "settings.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory holds %v, want only settings.json", names)
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "settings.json")
	s := New(path)

	if err := s.Save(domain.Settings{OriginID: "1080"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("settings file was not created: %v", err)
	}
}
