package domain

import "time"

// Settings are the six trip parameters the user configures live, over
// Telegram, rather than by editing config.yaml (see README "Configuring via
// Telegram"). Everything else the brief needs — risk margin, the train-type
// filter, certificate rules, and so on — stays in config.yaml, because those
// are calibration knobs rather than facts about a particular morning.
type Settings struct {
	// ScheduleWeekdays and ScheduleAt together decide when the brief fires.
	// Neither means anything without the other, so they are always set
	// together (/schedule).
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

// Complete reports whether every field needed to run a brief has been set,
// and names whichever are still missing so /status and the incomplete-config
// reminder can say exactly what to do next.
//
// The zero TimeOfday{00:00} doubles as "unset": no commute plan needs a
// midnight ready time or deadline, so this is a safe sentinel rather than a
// real ambiguity.
func (s Settings) Complete() (bool, []string) {
	var missing []string
	if len(s.ScheduleWeekdays) == 0 || s.ScheduleAt == (TimeOfDay{}) {
		missing = append(missing, "schedule")
	}
	if s.ReadyAt == (TimeOfDay{}) {
		missing = append(missing, "ready")
	}
	if s.DeadlineAt == (TimeOfDay{}) {
		missing = append(missing, "deadline")
	}
	if s.MaxEarlyLeave <= 0 {
		missing = append(missing, "earlyleave")
	}
	if s.OriginID == "" || s.DestinationID == "" {
		missing = append(missing, "route")
	}
	return len(missing) == 0, missing
}

// Route builds the Route value the brief renders the header from.
func (s Settings) Route() Route {
	return Route{OriginName: s.OriginName, DestinationName: s.DestinationName}
}

// Scheduling builds a single-schedule Scheduling from the live settings, to
// be merged with the config-file-only guard parameters (tolerance, retry
// window, skip dates) by the caller.
func (s Settings) Schedule() Schedule {
	return Schedule{Name: "commute", Weekdays: s.ScheduleWeekdays, At: s.ScheduleAt}
}
