package domain

import (
	"testing"
	"time"
)

func commuteSettings(name string) Settings {
	return Settings{
		Name:             name,
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

func TestSettingsListFind(t *testing.T) {
	l := SettingsList{Schedules: []Settings{commuteSettings("上班通勤"), commuteSettings("下班通勤")}}

	if _, ok := l.Find("上班通勤"); !ok {
		t.Error("expected to find 上班通勤")
	}
	if _, ok := l.Find("不存在"); ok {
		t.Error("did not expect to find a schedule that was never added")
	}
}

func TestSettingsListNameTaken(t *testing.T) {
	l := SettingsList{Schedules: []Settings{commuteSettings("上班通勤")}}

	if !l.NameTaken("上班通勤", "") {
		t.Error("expected the existing name to be taken")
	}
	if l.NameTaken("下班通勤", "") {
		t.Error("did not expect an unused name to be taken")
	}
	// A rename must not collide with itself.
	if l.NameTaken("上班通勤", "上班通勤") {
		t.Error("a schedule's own current name must not count as taken against itself")
	}
}

func TestSettingsListUpsertAppendsNewName(t *testing.T) {
	var l SettingsList
	l = l.Upsert(commuteSettings("上班通勤"))

	if len(l.Schedules) != 1 {
		t.Fatalf("len = %d, want 1", len(l.Schedules))
	}
	if l.Schedules[0].Name != "上班通勤" {
		t.Errorf("name = %s, want 上班通勤", l.Schedules[0].Name)
	}
}

func TestSettingsListUpsertReplacesSameName(t *testing.T) {
	l := SettingsList{Schedules: []Settings{commuteSettings("上班通勤"), commuteSettings("下班通勤")}}

	edited := commuteSettings("上班通勤")
	edited.ReadyAt = TimeOfDay{Hour: 8, Minute: 10}
	l = l.Upsert(edited)

	if len(l.Schedules) != 2 {
		t.Fatalf("len = %d, want 2 (a replace, not an append)", len(l.Schedules))
	}
	got, ok := l.Find("上班通勤")
	if !ok || got.ReadyAt != (TimeOfDay{Hour: 8, Minute: 10}) {
		t.Errorf("got %+v, want the edited ReadyAt to have taken effect", got)
	}
}

func TestSettingsListUpsertDoesNotMutateOriginal(t *testing.T) {
	original := SettingsList{Schedules: []Settings{commuteSettings("上班通勤")}}
	edited := commuteSettings("上班通勤")
	edited.ReadyAt = TimeOfDay{Hour: 8, Minute: 10}
	_ = original.Upsert(edited)

	if got, _ := original.Find("上班通勤"); got.ReadyAt != (TimeOfDay{Hour: 8, Minute: 20}) {
		t.Errorf("original list was mutated: ReadyAt = %v", got.ReadyAt)
	}
}

func TestSettingsListRemove(t *testing.T) {
	l := SettingsList{Schedules: []Settings{commuteSettings("上班通勤"), commuteSettings("下班通勤")}}
	l = l.Remove("上班通勤")

	if len(l.Schedules) != 1 {
		t.Fatalf("len = %d, want 1", len(l.Schedules))
	}
	if _, ok := l.Find("上班通勤"); ok {
		t.Error("上班通勤 should have been removed")
	}
	if _, ok := l.Find("下班通勤"); !ok {
		t.Error("下班通勤 should still be present")
	}
}

func TestSettingsSchedule(t *testing.T) {
	s := commuteSettings("上班通勤")
	sch := s.Schedule()

	if sch.Name != "上班通勤" {
		t.Errorf("name = %s, want 上班通勤", sch.Name)
	}
	if sch.At != s.ScheduleAt {
		t.Errorf("at = %v, want %v", sch.At, s.ScheduleAt)
	}
	if len(sch.Weekdays) != 5 {
		t.Errorf("weekdays = %v, want 5", sch.Weekdays)
	}
}

func TestSettingsRoute(t *testing.T) {
	s := commuteSettings("上班通勤")
	r := s.Route()

	if r.OriginName != "桃園" || r.DestinationName != "臺北" {
		t.Errorf("route = %+v, want 桃園 -> 臺北", r)
	}
}
