// Package settingsfile persists the live trip settings (schedule, ready
// time, deadline, route, max early leave) as JSON on disk, so a value the
// user sets over Telegram survives a restart and is visible to the next
// tick without either side having to share memory.
//
// It mirrors internal/adapter/statefile deliberately: same atomic
// write-to-temp-then-rename save, same "a missing file is not an error"
// load, because a fresh install has no settings yet and that must not block
// /start from working.
package settingsfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// Store reads and writes the live settings at Path.
type Store struct{ Path string }

// New builds a store.
func New(path string) *Store { return &Store{Path: path} }

// wire is the on-disk shape. Weekdays and clock times are written as text
// rather than domain.TimeOfDay's numeric fields, so the file can be read by
// eye when a setting looks wrong.
type wire struct {
	ScheduleWeekdays []string `json:"schedule_weekdays"`
	ScheduleAt       string   `json:"schedule_at"`
	ReadyAt          string   `json:"ready_at"`
	DeadlineAt       string   `json:"deadline_at"`
	MaxEarlyLeaveMin int      `json:"max_early_leave_minutes"`
	OriginID         string   `json:"origin_id"`
	OriginName       string   `json:"origin_name"`
	DestinationID    string   `json:"destination_id"`
	DestinationName  string   `json:"destination_name"`
}

// Load reads the settings. A missing file is not an error: it is the normal
// state of a fresh install, before /route, /ready, /deadline, /schedule and
// /earlyleave have been used for the first time.
func (s *Store) Load() (domain.Settings, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.Settings{}, nil
	}
	if err != nil {
		return domain.Settings{}, fmt.Errorf("read settings: %w", err)
	}

	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return domain.Settings{}, fmt.Errorf("decode settings: %w", err)
	}

	out := domain.Settings{
		MaxEarlyLeave:   time.Duration(w.MaxEarlyLeaveMin) * time.Minute,
		OriginID:        w.OriginID,
		OriginName:      w.OriginName,
		DestinationID:   w.DestinationID,
		DestinationName: w.DestinationName,
	}
	for _, name := range w.ScheduleWeekdays {
		wd, err := domain.ParseWeekday(name)
		if err != nil {
			return domain.Settings{}, fmt.Errorf("decode settings: %w", err)
		}
		out.ScheduleWeekdays = append(out.ScheduleWeekdays, wd)
	}
	if w.ScheduleAt != "" {
		t, err := domain.ParseTimeOfDay(w.ScheduleAt)
		if err != nil {
			return domain.Settings{}, fmt.Errorf("decode settings: schedule_at: %w", err)
		}
		out.ScheduleAt = t
	}
	if w.ReadyAt != "" {
		t, err := domain.ParseTimeOfDay(w.ReadyAt)
		if err != nil {
			return domain.Settings{}, fmt.Errorf("decode settings: ready_at: %w", err)
		}
		out.ReadyAt = t
	}
	if w.DeadlineAt != "" {
		t, err := domain.ParseTimeOfDay(w.DeadlineAt)
		if err != nil {
			return domain.Settings{}, fmt.Errorf("decode settings: deadline_at: %w", err)
		}
		out.DeadlineAt = t
	}
	return out, nil
}

// Save writes the settings atomically: a torn file after a crash would make
// every tick for the rest of the day read garbage, and Telegram commands can
// arrive at any moment, not just at a quiet time of day.
func (s *Store) Save(set domain.Settings) error {
	w := wire{
		ScheduleAt:       set.ScheduleAt.String(),
		ReadyAt:          set.ReadyAt.String(),
		DeadlineAt:       set.DeadlineAt.String(),
		MaxEarlyLeaveMin: int(set.MaxEarlyLeave / time.Minute),
		OriginID:         set.OriginID,
		OriginName:       set.OriginName,
		DestinationID:    set.DestinationID,
		DestinationName:  set.DestinationName,
	}
	// An unset TimeOfDay must round-trip as absent, not as "00:00" — that
	// would otherwise be misread as a real midnight setting on the next load.
	if set.ScheduleAt == (domain.TimeOfDay{}) {
		w.ScheduleAt = ""
	}
	if set.ReadyAt == (domain.TimeOfDay{}) {
		w.ReadyAt = ""
	}
	if set.DeadlineAt == (domain.TimeOfDay{}) {
		w.DeadlineAt = ""
	}
	for _, wd := range set.ScheduleWeekdays {
		w.ScheduleWeekdays = append(w.ScheduleWeekdays, domain.WeekdayShort(wd))
	}

	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".settings-*.json")
	if err != nil {
		return fmt.Errorf("create temp settings: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp settings: %w", err)
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		return fmt.Errorf("replace settings: %w", err)
	}
	return nil
}
