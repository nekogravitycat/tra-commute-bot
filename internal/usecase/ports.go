package usecase

import (
	"context"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// Clock supplies the current time. It is a port rather than a direct call to
// time.Now so that -at can simulate any morning: a program that runs once a day
// is otherwise undebuggable, because every mistake costs 24 hours to reproduce.
type Clock interface {
	Now() time.Time
}

// Timetable is one day's origin-to-destination schedule, with times already
// resolved to absolute instants.
type Timetable struct {
	ServiceDate time.Time
	UpdatedAt   time.Time
	Services    []domain.Service
}

// DelaySnapshot is the live delay of every running train, in minutes, keyed by
// train number. Values may be negative; clamping is the domain's job.
type DelaySnapshot struct {
	UpdatedAt time.Time
	ByTrainNo map[string]int
}

// TimetableSource provides the day's scheduled services for one leg.
type TimetableSource interface {
	DailyODTimetable(ctx context.Context, originID, destID string, date time.Time) (Timetable, error)
}

// DelaySource provides current delays for all running trains.
type DelaySource interface {
	LiveDelays(ctx context.Context) (DelaySnapshot, error)
}

// Message is a rendered notification, ready to send.
type Message struct {
	Text      string
	ParseMode string
}

// Renderer turns a brief into a message. Keeping this behind a port is what
// lets -dry-run print the exact bytes that would have been sent, rather than an
// approximation of them.
type Renderer interface {
	Render(domain.Brief) Message
}

// Notifier delivers a message.
type Notifier interface {
	Send(ctx context.Context, m Message) error
}

// StateStore persists the tick guard state across the 1440 daily processes.
type StateStore interface {
	Load() (domain.TickState, error)
	Save(domain.TickState) error
}

// SettingsStore persists every Schedule (§10.1) the user has set up over
// Telegram. It is read fresh at the top of every tick and written by the
// Telegram command handler (through the settings actor, §10.9), so a change
// takes effect on the very next wake-up with no restart.
type SettingsStore interface {
	Load() (domain.SettingsList, error)
	Save(domain.SettingsList) error
}

// Archiver keeps the raw API responses. When a recommendation turns out to
// have been wrong, this archive is the only way to find out why.
type Archiver interface {
	Archive(name string, payload []byte) error
}
