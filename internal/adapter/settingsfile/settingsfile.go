// Package settingsfile persists every Schedule (§10.1) the user has set up
// over Telegram — name, notify weekdays/time, route, ready time, deadline,
// max early leave — as JSON on disk, so settings.json survives a restart and
// is visible to the next tick without either side having to share memory.
//
// It mirrors internal/adapter/statefile deliberately: same atomic
// write-to-temp-then-rename save, same "a missing file is not an error"
// load, because a fresh install has no schedules yet and that must not block
// /setup from working.
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

// Store reads and writes settings.json at Path.
type Store struct{ Path string }

// New builds a store.
func New(path string) *Store { return &Store{Path: path} }

// wireList is settings.json's on-disk shape (§10.8): a list under
// "schedules", rather than v0.1's single object, so /setup can add a second
// Schedule without the file's shape changing. usual_train_nos sits alongside
// it rather than inside any one Schedule, since it is shared by all of them
// (§8, §10.1) and set live via /usualtrain rather than config.yaml.
type wireList struct {
	Schedules     []wireSchedule `json:"schedules"`
	UsualTrainNos []string       `json:"usual_train_nos"`
}

// wireSchedule is one Schedule. Weekdays and clock times are written as text
// rather than domain.TimeOfDay's numeric fields, so the file can be read by
// eye when a setting looks wrong.
type wireSchedule struct {
	Name             string   `json:"name"`
	NotifyWeekdays   []string `json:"notify_weekdays"`
	NotifyAt         string   `json:"notify_at"`
	OriginID         string   `json:"origin_id"`
	OriginName       string   `json:"origin_name"`
	DestinationID    string   `json:"destination_id"`
	DestinationName  string   `json:"destination_name"`
	ReadyAt          string   `json:"ready_at"`
	DeadlineAt       string   `json:"deadline_at"`
	MaxEarlyLeaveMin int      `json:"max_early_leave_minutes"`
}

// Load reads every Schedule. A missing file is not an error: it is the
// normal state of a fresh install, before /setup has been used for the first
// time.
func (s *Store) Load() (domain.SettingsList, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.SettingsList{}, nil
	}
	if err != nil {
		return domain.SettingsList{}, fmt.Errorf("read settings: %w", err)
	}

	var w wireList
	if err := json.Unmarshal(data, &w); err != nil {
		return domain.SettingsList{}, fmt.Errorf("decode settings: %w", err)
	}

	list := domain.SettingsList{
		Schedules:     make([]domain.Settings, 0, len(w.Schedules)),
		UsualTrainNos: w.UsualTrainNos,
	}
	for _, ws := range w.Schedules {
		set, err := ws.toDomain()
		if err != nil {
			return domain.SettingsList{}, fmt.Errorf("decode settings: schedule %q: %w", ws.Name, err)
		}
		list.Schedules = append(list.Schedules, set)
	}
	return list, nil
}

func (ws wireSchedule) toDomain() (domain.Settings, error) {
	out := domain.Settings{
		Name:            ws.Name,
		MaxEarlyLeave:   time.Duration(ws.MaxEarlyLeaveMin) * time.Minute,
		OriginID:        ws.OriginID,
		OriginName:      ws.OriginName,
		DestinationID:   ws.DestinationID,
		DestinationName: ws.DestinationName,
	}
	for _, name := range ws.NotifyWeekdays {
		wd, err := domain.ParseWeekday(name)
		if err != nil {
			return domain.Settings{}, err
		}
		out.ScheduleWeekdays = append(out.ScheduleWeekdays, wd)
	}
	if ws.NotifyAt != "" {
		t, err := domain.ParseTimeOfDay(ws.NotifyAt)
		if err != nil {
			return domain.Settings{}, fmt.Errorf("notify_at: %w", err)
		}
		out.ScheduleAt = t
	}
	if ws.ReadyAt != "" {
		t, err := domain.ParseTimeOfDay(ws.ReadyAt)
		if err != nil {
			return domain.Settings{}, fmt.Errorf("ready_at: %w", err)
		}
		out.ReadyAt = t
	}
	if ws.DeadlineAt != "" {
		t, err := domain.ParseTimeOfDay(ws.DeadlineAt)
		if err != nil {
			return domain.Settings{}, fmt.Errorf("deadline_at: %w", err)
		}
		out.DeadlineAt = t
	}
	return out, nil
}

func fromDomain(set domain.Settings) wireSchedule {
	w := wireSchedule{
		Name:             set.Name,
		NotifyAt:         set.ScheduleAt.String(),
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
		w.NotifyAt = ""
	}
	if set.ReadyAt == (domain.TimeOfDay{}) {
		w.ReadyAt = ""
	}
	if set.DeadlineAt == (domain.TimeOfDay{}) {
		w.DeadlineAt = ""
	}
	for _, wd := range set.ScheduleWeekdays {
		w.NotifyWeekdays = append(w.NotifyWeekdays, domain.WeekdayShort(wd))
	}
	return w
}

// Save writes every Schedule atomically: a torn file after a crash would
// make every tick for the rest of the day read garbage, and Telegram
// commands can arrive at any moment, not just at a quiet time of day.
func (s *Store) Save(list domain.SettingsList) error {
	w := wireList{
		Schedules:     make([]wireSchedule, 0, len(list.Schedules)),
		UsualTrainNos: list.UsualTrainNos,
	}
	for _, set := range list.Schedules {
		w.Schedules = append(w.Schedules, fromDomain(set))
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
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp settings: %w", err)
	}
	// Flushed to disk before the rename that makes it visible — otherwise a
	// crash between the rename and the OS's own lazy flush can leave the
	// now-current file empty or truncated, which is exactly the torn write
	// this atomic-rename dance exists to prevent in the first place.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp settings: %w", err)
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		return fmt.Errorf("replace settings: %w", err)
	}
	return nil
}
