package domain

import "time"

// Settings is one Schedule (§10.1): the complete set of trip parameters the
// user configures live over Telegram, via /setup and /manage, rather than by
// editing config.yaml. Everything else the brief needs — risk margin, the
// train-type filter, certificate rules, and so on — stays in config.yaml,
// because those are calibration knobs shared by every Schedule rather than
// facts about one particular commute.
//
// A Schedule has exactly two states: absent, or fully populated. /setup only
// writes one once every field below has been collected (§10.2 invariant 1),
// so nothing in this package needs to reason about a partially configured
// Schedule.
type Settings struct {
	// Name is the user's label for this rule (e.g. "上班通勤"), unique within
	// a SettingsList. It is also the key used in state.json and in the guard
	// (Schedule.Name), so a rename is a delete-and-recreate under the hood as
	// far as the tick guard's history is concerned.
	Name string

	// ScheduleWeekdays and ScheduleAt together decide when the brief fires.
	// Neither means anything without the other, so they are always set
	// together, in the same step of /setup or /manage's "改通知時間".
	ScheduleWeekdays []time.Weekday
	ScheduleAt       TimeOfDay

	// ReadyAt is T_ready: the earliest the user can be standing at the
	// origin station. It is an absolute clock time the user states directly,
	// independent of ScheduleAt.
	ReadyAt TimeOfDay
	// DeadlineAt is the latest acceptable arrival at the destination
	// station.
	DeadlineAt TimeOfDay

	// MaxEarlyLeave caps how much earlier a compensation option may ask the
	// user to leave (§7.8).
	MaxEarlyLeave time.Duration

	OriginID        string
	OriginName      string
	DestinationID   string
	DestinationName string
}

// Route builds the Route value the brief renders the header from.
func (s Settings) Route() Route {
	return Route{OriginName: s.OriginName, DestinationName: s.DestinationName}
}

// Schedule builds the guard-only view of this Settings row — the weekdays and
// fire time DecideTicks matches against — to be merged with the config-file
// guard parameters (tolerance, retry window, skip/extra dates) by the caller.
func (s Settings) Schedule() Schedule {
	return Schedule{Name: s.Name, Weekdays: s.ScheduleWeekdays, At: s.ScheduleAt}
}

// SettingsList is every Schedule the user has configured, the on-disk shape
// of settings.json (§10.8). Order is preserved across Save/Load so /manage's
// listing stays stable between edits.
type SettingsList struct {
	Schedules []Settings
}

// Find returns the named Schedule, if any.
func (l SettingsList) Find(name string) (Settings, bool) {
	for _, s := range l.Schedules {
		if s.Name == name {
			return s, true
		}
	}
	return Settings{}, false
}

// NameTaken reports whether name is already used by another Schedule. Passing
// the Schedule's own current name as except lets a rename check against every
// other name without tripping on itself.
func (l SettingsList) NameTaken(name, except string) bool {
	for _, s := range l.Schedules {
		if s.Name == name && s.Name != except {
			return true
		}
	}
	return false
}

// Upsert returns a copy of the list with s replacing the Schedule of the same
// name, or appended if no such Schedule exists yet. This is the single write
// path both /setup's "確認建立" and /manage's field edits go through, so a
// Schedule is always replaced whole (§10.2 invariant 1) — never patched
// field-by-field in place.
func (l SettingsList) Upsert(s Settings) SettingsList {
	out := SettingsList{Schedules: make([]Settings, len(l.Schedules))}
	copy(out.Schedules, l.Schedules)
	for i, existing := range out.Schedules {
		if existing.Name == s.Name {
			out.Schedules[i] = s
			return out
		}
	}
	out.Schedules = append(out.Schedules, s)
	return out
}

// Remove returns a copy of the list with the named Schedule deleted.
func (l SettingsList) Remove(name string) SettingsList {
	out := SettingsList{Schedules: make([]Settings, 0, len(l.Schedules))}
	for _, s := range l.Schedules {
		if s.Name != name {
			out.Schedules = append(out.Schedules, s)
		}
	}
	return out
}
