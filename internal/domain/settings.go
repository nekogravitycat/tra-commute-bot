package domain

import (
	"slices"
	"strings"
	"time"
)

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

// Shortcut is one quick on-demand query (§10.x): a trigger word that, sent as
// a plain chat message, answers with the live board for one route right now
// rather than waiting for a Schedule's own notify time. Unlike a Schedule it
// carries no ready time, deadline or notify schedule — it isn't a commute
// rule to fire later, it's a question to answer immediately.
type Shortcut struct {
	// Trigger is matched case-insensitively against an incoming message
	// (see SettingsList.FindShortcutByTrigger), so "home" and "Home" both
	// fire it.
	Trigger         string
	OriginID        string
	OriginName      string
	DestinationID   string
	DestinationName string
}

// Route builds the Route value a shortcut's board query renders the header
// from, the same shape Settings.Route builds for a scheduled brief.
func (s Shortcut) Route() Route {
	return Route{OriginName: s.OriginName, DestinationName: s.DestinationName}
}

// SettingsList is every Schedule the user has configured, the on-disk shape
// of settings.json (§10.8). Order is preserved across Save/Load so /manage's
// listing stays stable between edits.
type SettingsList struct {
	Schedules []Settings

	// UsualTrainNos are the user's habitual train numbers (§2.2, §8): a train
	// on this list is always shown even if the ranking would otherwise drop
	// it from the candidate table. Shared across every Schedule rather than
	// per-route — a train number only ever runs in one direction, so a
	// shared list does not misfire against an unrelated Schedule (see
	// spec.md §8's note). Set live via /usualtrain rather than config.yaml,
	// so it takes effect on the next tick without a restart.
	UsualTrainNos []string

	// Shortcuts are the user's quick on-demand queries, set live via
	// /shortcuts. Like UsualTrainNos this is its own top-level list rather
	// than nested under any one Schedule, since a shortcut answers a
	// question about a route, not about a commute rule.
	Shortcuts []Shortcut
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
	return slices.ContainsFunc(l.Schedules, func(s Settings) bool {
		return s.Name == name && s.Name != except
	})
}

// Upsert returns a copy of the list with s replacing the Schedule of the same
// name, or appended if no such Schedule exists yet. This is the single write
// path both /setup's "確認建立" and /manage's field edits go through, so a
// Schedule is always replaced whole (§10.2 invariant 1) — never patched
// field-by-field in place.
//
// Every method here returns a list that shares no backing array with the
// receiver, so a caller can never discover which of them happens to copy.
func (l SettingsList) Upsert(s Settings) SettingsList {
	out := SettingsList{
		Schedules:     slices.Clone(l.Schedules),
		UsualTrainNos: slices.Clone(l.UsualTrainNos),
		Shortcuts:     slices.Clone(l.Shortcuts),
	}
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
	return SettingsList{
		Schedules: slices.DeleteFunc(slices.Clone(l.Schedules), func(s Settings) bool {
			return s.Name == name
		}),
		UsualTrainNos: slices.Clone(l.UsualTrainNos),
		Shortcuts:     slices.Clone(l.Shortcuts),
	}
}

// AddUsualTrain returns a copy of the list with no marked habitual. A train
// already on the list is left alone rather than duplicated.
func (l SettingsList) AddUsualTrain(no string) SettingsList {
	if slices.Contains(l.UsualTrainNos, no) {
		return l
	}
	return SettingsList{
		Schedules:     slices.Clone(l.Schedules),
		UsualTrainNos: append(slices.Clone(l.UsualTrainNos), no),
		Shortcuts:     slices.Clone(l.Shortcuts),
	}
}

// RemoveUsualTrain returns a copy of the list with no no longer marked
// habitual. Removing a train not on the list is a no-op.
func (l SettingsList) RemoveUsualTrain(no string) SettingsList {
	return SettingsList{
		Schedules: slices.Clone(l.Schedules),
		UsualTrainNos: slices.DeleteFunc(slices.Clone(l.UsualTrainNos), func(existing string) bool {
			return existing == no
		}),
		Shortcuts: slices.Clone(l.Shortcuts),
	}
}

// FindShortcutByTrigger matches an incoming plain-text message against every
// configured Shortcut, case-insensitively — "home" and "Home" both fire the
// same shortcut, since a phone keyboard's autocapitalisation should not be
// the difference between a hit and a miss.
func (l SettingsList) FindShortcutByTrigger(text string) (Shortcut, bool) {
	q := strings.ToLower(strings.TrimSpace(text))
	if q == "" {
		return Shortcut{}, false
	}
	for _, s := range l.Shortcuts {
		if strings.ToLower(s.Trigger) == q {
			return s, true
		}
	}
	return Shortcut{}, false
}

// TriggerTaken reports whether trigger is already used by another Shortcut,
// case-insensitively.
func (l SettingsList) TriggerTaken(trigger string) bool {
	q := strings.ToLower(trigger)
	return slices.ContainsFunc(l.Shortcuts, func(s Shortcut) bool {
		return strings.ToLower(s.Trigger) == q
	})
}

// AddShortcut returns a copy of the list with s appended. Unlike Settings'
// Upsert, a shortcut is never edited in place (§10.x: /shortcuts only adds
// and deletes) — a trigger already taken is rejected by the caller before
// this is reached, via TriggerTaken.
func (l SettingsList) AddShortcut(s Shortcut) SettingsList {
	return SettingsList{
		Schedules:     slices.Clone(l.Schedules),
		UsualTrainNos: slices.Clone(l.UsualTrainNos),
		Shortcuts:     append(slices.Clone(l.Shortcuts), s),
	}
}

// RemoveShortcut returns a copy of the list with the named trigger's
// Shortcut deleted, matched case-insensitively like FindShortcutByTrigger.
func (l SettingsList) RemoveShortcut(trigger string) SettingsList {
	q := strings.ToLower(trigger)
	return SettingsList{
		Schedules:     slices.Clone(l.Schedules),
		UsualTrainNos: slices.Clone(l.UsualTrainNos),
		Shortcuts: slices.DeleteFunc(slices.Clone(l.Shortcuts), func(s Shortcut) bool {
			return strings.ToLower(s.Trigger) == q
		}),
	}
}
