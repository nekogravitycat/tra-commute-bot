package settingsfile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

func fullSchedule(name string) domain.Settings {
	return domain.Settings{
		Name:             name,
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
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := New(path)

	want := domain.SettingsList{
		Schedules: []domain.Settings{
			fullSchedule("上班通勤"),
			fullSchedule("下班通勤"),
		},
		UsualTrainNos: []string{"2008", "1136", "1138"},
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
}

// TestLoadMissingFile checks a fresh install starts cleanly, with an empty
// list rather than an error — the normal state before /setup has ever run.
func TestLoadMissingFile(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "absent.json"))

	got, err := s.Load()
	if err != nil {
		t.Fatalf("a missing settings file should not be an error: %v", err)
	}
	if len(got.Schedules) != 0 {
		t.Errorf("fresh settings should hold no schedules, got %+v", got)
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
// absent rather than as a literal 00:00.
func TestUnsetTimesDoNotBecomeMidnight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := New(path)

	list := domain.SettingsList{Schedules: []domain.Settings{
		{Name: "上班通勤", OriginID: "1080", DestinationID: "1000"},
	}}
	if err := s.Save(list); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	set, ok := got.Find("上班通勤")
	if !ok {
		t.Fatalf("expected to find 上班通勤 in %+v", got)
	}
	if set.ReadyAt != (domain.TimeOfDay{}) || set.DeadlineAt != (domain.TimeOfDay{}) {
		t.Errorf("ready/deadline = %+v/%+v, want both unset", set.ReadyAt, set.DeadlineAt)
	}
}

// TestSaveIsAtomic checks no partial file is left behind after several saves.
func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	s := New(path)

	for i := 0; i < 3; i++ {
		list := domain.SettingsList{Schedules: []domain.Settings{{Name: "上班通勤", OriginID: "1080"}}}
		if err := s.Save(list); err != nil {
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

	list := domain.SettingsList{Schedules: []domain.Settings{{Name: "上班通勤", OriginID: "1080"}}}
	if err := s.Save(list); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("settings file was not created: %v", err)
	}
}

// TestSaveEmptyListRoundTrips checks deleting every schedule (via /manage)
// produces a valid, re-loadable empty file rather than a decode error.
func TestSaveEmptyListRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := New(path)

	if err := s.Save(domain.SettingsList{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Schedules) != 0 {
		t.Errorf("got %+v, want an empty list", got)
	}
}
