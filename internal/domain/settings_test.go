package domain

import (
	"testing"
	"time"
)

func completeSettings() Settings {
	return Settings{
		ScheduleWeekdays: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
		ScheduleAt:       TimeOfDay{Hour: 7, Minute: 50},
		ReadyAt:          TimeOfDay{Hour: 8, Minute: 20},
		DeadlineAt:       TimeOfDay{Hour: 9, Minute: 10},
		MaxEarlyLeave:    15 * time.Minute,
		OriginID:         "1080",
		OriginName:       "桃園",
		DestinationID:    "1000",
		DestinationName:  "臺北",
	}
}

func TestSettingsCompleteWhenFullySet(t *testing.T) {
	complete, missing := completeSettings().Complete()
	if !complete || len(missing) != 0 {
		t.Errorf("complete = %v, missing = %v, want true and none", complete, missing)
	}
}

func TestSettingsIncompleteNamesEachMissingField(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Settings)
		missing string
	}{
		{"no schedule", func(s *Settings) { s.ScheduleWeekdays = nil }, "schedule"},
		{"no ready", func(s *Settings) { s.ReadyAt = TimeOfDay{} }, "ready"},
		{"no deadline", func(s *Settings) { s.DeadlineAt = TimeOfDay{} }, "deadline"},
		{"no early leave", func(s *Settings) { s.MaxEarlyLeave = 0 }, "earlyleave"},
		{"no route", func(s *Settings) { s.OriginID = "" }, "route"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := completeSettings()
			tc.mutate(&s)
			complete, missing := s.Complete()
			if complete {
				t.Fatal("expected incomplete settings")
			}
			found := false
			for _, m := range missing {
				if m == tc.missing {
					found = true
				}
			}
			if !found {
				t.Errorf("missing = %v, want it to include %q", missing, tc.missing)
			}
		})
	}
}

func TestSettingsZeroValueIsIncomplete(t *testing.T) {
	complete, missing := Settings{}.Complete()
	if complete || len(missing) == 0 {
		t.Errorf("zero-value settings should be incomplete with named gaps, got complete=%v missing=%v", complete, missing)
	}
}
