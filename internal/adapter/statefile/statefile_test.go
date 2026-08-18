package statefile

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

var testLoc = time.FixedZone("Asia/Taipei", 8*3600)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := New(path)

	lastAt := time.Date(2026, 8, 18, 7, 50, 3, 0, testLoc)
	want := domain.TickState{
		LastSuccess: map[string]string{"平日通勤": "2026-08-18"},
		Attempts: map[string]domain.Attempt{
			"平日通勤": {Date: "2026-08-18", Count: 2, LastAt: lastAt, GaveUp: true},
		},
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LastSuccess["平日通勤"] != "2026-08-18" {
		t.Errorf("last success = %q, want 2026-08-18", got.LastSuccess["平日通勤"])
	}
	a := got.Attempts["平日通勤"]
	if a.Count != 2 || a.Date != "2026-08-18" || !a.GaveUp {
		t.Errorf("attempt = %+v, want two attempts on 2026-08-18 with the give-up flag", a)
	}
	if !a.LastAt.Equal(lastAt) {
		t.Errorf("last attempt = %s, want %s", a.LastAt, lastAt)
	}
}

// TestLoadMissingFile checks a fresh install starts cleanly. Treating an absent
// file as a failure would block the very first brief.
func TestLoadMissingFile(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "absent.json"))

	got, err := s.Load()
	if err != nil {
		t.Fatalf("a missing state file should not be an error: %v", err)
	}
	if len(got.LastSuccess) != 0 || len(got.Attempts) != 0 {
		t.Errorf("state = %+v, want empty", got)
	}
}

func TestLoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := New(path).Load(); err == nil {
		t.Error("expected a decode error for a corrupt state file")
	}
}

// TestSaveCreatesDirectory checks the first run on a fresh host works even
// before anything has created the state directory.
func TestSaveCreatesDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "state.json")
	s := New(path)

	if err := s.Save(domain.TickState{LastSuccess: map[string]string{"x": "2026-08-18"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("state file was not created: %v", err)
	}
}

// TestSaveIsAtomic checks no partial file is left behind. A torn state file
// would make the guard read garbage on every tick for the rest of the day.
func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := New(path)

	for i := 0; i < 3; i++ {
		if err := s.Save(domain.TickState{LastSuccess: map[string]string{"x": "2026-08-18"}}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory holds %v, want only state.json", names)
	}
}

// TestSaveEmptyState checks the zero value survives a round trip, which is the
// state every fresh install starts from.
func TestSaveEmptyState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := New(path)

	if err := s.Save(domain.TickState{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Attempts) != 0 {
		t.Errorf("attempts = %+v, want none", got.Attempts)
	}
}

// TestStaleAttemptDateDerived checks the attempt date is taken from the stored
// timestamp, so the two can never contradict each other.
func TestStaleAttemptDateDerived(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	body := `{
	  "last_success": {},
	  "last_attempt": {"平日通勤": "2026-08-17T07:50:00+08:00"},
	  "attempt_count_today": {"平日通勤": 5},
	  "gave_up": {"平日通勤": "2026-08-17"}
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := New(path).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	a := got.Attempts["平日通勤"]
	if a.Date != "2026-08-17" {
		t.Errorf("attempt date = %q, want it derived from the timestamp", a.Date)
	}

	// Yesterday's counters must not gate today.
	today := time.Date(2026, 8, 18, 7, 50, 0, 0, testLoc)
	if fresh := got.AttemptOn("平日通勤", today); fresh.Count != 0 {
		t.Errorf("today's attempt count = %d, want 0", fresh.Count)
	}
}
