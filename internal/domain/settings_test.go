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

func TestSettingsListAddUsualTrainDedupes(t *testing.T) {
	var l SettingsList
	l = l.AddUsualTrain("2008")
	l = l.AddUsualTrain("1136")
	l = l.AddUsualTrain("2008") // already present: must not duplicate

	if len(l.UsualTrainNos) != 2 {
		t.Fatalf("usual train nos = %v, want 2 entries", l.UsualTrainNos)
	}
}

func TestSettingsListAddUsualTrainDoesNotMutateOriginal(t *testing.T) {
	original := SettingsList{UsualTrainNos: []string{"2008"}}
	_ = original.AddUsualTrain("1136")

	if len(original.UsualTrainNos) != 1 {
		t.Errorf("original list was mutated: %v", original.UsualTrainNos)
	}
}

func TestSettingsListRemoveUsualTrain(t *testing.T) {
	l := SettingsList{UsualTrainNos: []string{"2008", "1136", "1138"}}
	l = l.RemoveUsualTrain("1136")

	want := []string{"2008", "1138"}
	if len(l.UsualTrainNos) != len(want) {
		t.Fatalf("usual train nos = %v, want %v", l.UsualTrainNos, want)
	}
	for i, no := range want {
		if l.UsualTrainNos[i] != no {
			t.Errorf("usual train nos = %v, want %v", l.UsualTrainNos, want)
		}
	}
}

// TestSettingsListRemoveUsualTrainAbsentIsNoOp checks removing a train number
// that was never added does not error or panic — /usualtrain's delete button
// can only ever offer numbers already on the list, but a concurrent removal
// from another session must still fail safely.
func TestSettingsListRemoveUsualTrainAbsentIsNoOp(t *testing.T) {
	l := SettingsList{UsualTrainNos: []string{"2008"}}
	l = l.RemoveUsualTrain("9999")

	if len(l.UsualTrainNos) != 1 || l.UsualTrainNos[0] != "2008" {
		t.Errorf("usual train nos = %v, want unchanged [2008]", l.UsualTrainNos)
	}
}

// TestSettingsListUpsertPreservesUsualTrainNos guards against a regression
// where writing a Schedule (via /setup or /manage) would silently wipe the
// unrelated, list-wide usual-train marker.
func TestSettingsListUpsertPreservesUsualTrainNos(t *testing.T) {
	l := SettingsList{UsualTrainNos: []string{"2008", "1136"}}
	l = l.Upsert(commuteSettings("上班通勤"))

	if len(l.UsualTrainNos) != 2 {
		t.Errorf("usual train nos = %v, want unchanged [2008 1136]", l.UsualTrainNos)
	}
}

// TestSettingsListRemovePreservesUsualTrainNos is the same guard as above,
// for the /manage delete-Schedule path.
func TestSettingsListRemovePreservesUsualTrainNos(t *testing.T) {
	l := SettingsList{
		Schedules:     []Settings{commuteSettings("上班通勤")},
		UsualTrainNos: []string{"2008", "1136"},
	}
	l = l.Remove("上班通勤")

	if len(l.UsualTrainNos) != 2 {
		t.Errorf("usual train nos = %v, want unchanged [2008 1136]", l.UsualTrainNos)
	}
}

func homeShortcut() Shortcut {
	return Shortcut{Trigger: "home", OriginID: "1000", OriginName: "臺北", DestinationID: "1080", DestinationName: "桃園"}
}

func TestShortcutRoute(t *testing.T) {
	r := homeShortcut().Route()
	if r.OriginName != "臺北" || r.DestinationName != "桃園" {
		t.Errorf("route = %+v, want 臺北 -> 桃園", r)
	}
}

func TestSettingsListFindShortcutByTriggerCaseInsensitive(t *testing.T) {
	l := SettingsList{Shortcuts: []Shortcut{homeShortcut()}}

	for _, q := range []string{"home", "Home", "HOME", " home "} {
		if _, ok := l.FindShortcutByTrigger(q); !ok {
			t.Errorf("FindShortcutByTrigger(%q) did not match, want a case-insensitive hit", q)
		}
	}
	if _, ok := l.FindShortcutByTrigger("office"); ok {
		t.Error("did not expect a match for an unconfigured trigger")
	}
}

func TestSettingsListTriggerTakenCaseInsensitive(t *testing.T) {
	l := SettingsList{Shortcuts: []Shortcut{homeShortcut()}}

	if !l.TriggerTaken("Home") {
		t.Error("expected the existing trigger to be taken, case-insensitively")
	}
	if l.TriggerTaken("office") {
		t.Error("did not expect an unused trigger to be taken")
	}
}

func TestSettingsListAddShortcut(t *testing.T) {
	var l SettingsList
	l = l.AddShortcut(homeShortcut())

	if len(l.Shortcuts) != 1 || l.Shortcuts[0].Trigger != "home" {
		t.Fatalf("shortcuts = %+v, want [home]", l.Shortcuts)
	}
}

func TestSettingsListRemoveShortcut(t *testing.T) {
	l := SettingsList{Shortcuts: []Shortcut{
		homeShortcut(),
		{Trigger: "office", OriginID: "1080", OriginName: "桃園", DestinationID: "1000", DestinationName: "臺北"},
	}}
	l = l.RemoveShortcut("HOME")

	if len(l.Shortcuts) != 1 || l.Shortcuts[0].Trigger != "office" {
		t.Fatalf("shortcuts = %+v, want only office left", l.Shortcuts)
	}
}

// TestSettingsListUpsertPreservesShortcuts and
// TestSettingsListRemovePreservesShortcuts guard the same class of
// regression as the UsualTrainNos tests above: writing or deleting a
// Schedule must not silently wipe the unrelated Shortcuts list.
func TestSettingsListUpsertPreservesShortcuts(t *testing.T) {
	l := SettingsList{Shortcuts: []Shortcut{homeShortcut()}}
	l = l.Upsert(commuteSettings("上班通勤"))

	if len(l.Shortcuts) != 1 {
		t.Errorf("shortcuts = %+v, want unchanged [home]", l.Shortcuts)
	}
}

func TestSettingsListRemovePreservesShortcuts(t *testing.T) {
	l := SettingsList{
		Schedules: []Settings{commuteSettings("上班通勤")},
		Shortcuts: []Shortcut{homeShortcut()},
	}
	l = l.Remove("上班通勤")

	if len(l.Shortcuts) != 1 {
		t.Errorf("shortcuts = %+v, want unchanged [home]", l.Shortcuts)
	}
}

// TestSettingsListAddShortcutPreservesUsualTrainNos guards the reverse
// direction: adding a shortcut must not disturb the unrelated usual-train
// list.
func TestSettingsListAddShortcutPreservesUsualTrainNos(t *testing.T) {
	l := SettingsList{UsualTrainNos: []string{"2008"}}
	l = l.AddShortcut(homeShortcut())

	if len(l.UsualTrainNos) != 1 || l.UsualTrainNos[0] != "2008" {
		t.Errorf("usual train nos = %v, want unchanged [2008]", l.UsualTrainNos)
	}
}
