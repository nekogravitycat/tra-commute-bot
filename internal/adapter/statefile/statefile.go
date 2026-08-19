// Package statefile persists the tick guard state as JSON on disk.
package statefile

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

// Store reads and writes the guard state at Path.
type Store struct{ Path string }

// New builds a store.
func New(path string) *Store { return &Store{Path: path} }

// wire is the on-disk shape. It is kept flat and human-readable because the
// first thing anyone does when the brief fails to arrive is cat this file.
type wire struct {
	LastSuccess       map[string]string    `json:"last_success"`
	LastAttempt       map[string]time.Time `json:"last_attempt"`
	AttemptCountToday map[string]int       `json:"attempt_count_today"`
	GaveUp            map[string]string    `json:"gave_up"`
}

// Load reads the state. A missing file is not an error: the first run of a
// fresh install has no history, and treating that as a failure would block the
// very first brief.
func (s *Store) Load() (domain.TickState, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.TickState{}, nil
	}
	if err != nil {
		return domain.TickState{}, fmt.Errorf("read state: %w", err)
	}

	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return domain.TickState{}, fmt.Errorf("decode state: %w", err)
	}

	st := domain.TickState{
		LastSuccess: w.LastSuccess,
		Attempts:    map[string]domain.Attempt{},
	}
	if st.LastSuccess == nil {
		st.LastSuccess = map[string]string{}
	}
	for name, at := range w.LastAttempt {
		// The attempt date is derived from the timestamp rather than stored
		// separately, so the two can never disagree.
		st.Attempts[name] = domain.Attempt{
			Date:   domain.DateKeyOf(at),
			Count:  w.AttemptCountToday[name],
			LastAt: at,
			GaveUp: w.GaveUp[name] == domain.DateKeyOf(at),
		}
	}
	return st, nil
}

// Save writes the state atomically: a torn state file after a crash would make
// the guard read garbage every minute for the rest of the day.
func (s *Store) Save(st domain.TickState) error {
	w := wire{
		LastSuccess:       st.LastSuccess,
		LastAttempt:       map[string]time.Time{},
		AttemptCountToday: map[string]int{},
		GaveUp:            map[string]string{},
	}
	if w.LastSuccess == nil {
		w.LastSuccess = map[string]string{}
	}
	for name, a := range st.Attempts {
		if a.Date == "" {
			continue
		}
		w.LastAttempt[name] = a.LastAt
		w.AttemptCountToday[name] = a.Count
		if a.GaveUp {
			w.GaveUp[name] = a.Date
		}
	}

	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".state-*.json")
	if err != nil {
		return fmt.Errorf("create temp state: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp state: %w", err)
	}
	// Flushed to disk before the rename that makes it visible — otherwise a
	// crash between the rename and the OS's own lazy flush can leave the
	// now-current file empty or truncated, which is exactly the torn write
	// this atomic-rename dance exists to prevent in the first place.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state: %w", err)
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}
