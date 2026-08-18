package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testLoc = time.FixedZone("Asia/Taipei", 8*3600)

func fixedNow(day int) func() time.Time {
	return func() time.Time { return time.Date(2026, 8, day, 7, 50, 0, 0, testLoc) }
}

func TestArchiveWritesPerDay(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, 30*24*time.Hour, fixedNow(18))

	if err := a.Archive("timetable", []byte(`{"TrainTimetables":[]}`)); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if err := a.Archive("liveboard", []byte(`{"TrainLiveBoards":[]}`)); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "2026-08-18.jsonl"))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("archive holds %d lines, want 2", len(lines))
	}

	var rec entry
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("decode entry: %v", err)
	}
	if rec.Name != "timetable" {
		t.Errorf("name = %q, want timetable", rec.Name)
	}
	if !strings.Contains(string(rec.Payload), "TrainTimetables") {
		t.Errorf("payload = %s, want the raw body", rec.Payload)
	}
}

// TestArchiveKeepsInvalidPayloads checks a non-JSON body is stored too. An HTML
// error page from a proxy is exactly the kind of thing worth having a copy of.
func TestArchiveKeepsInvalidPayloads(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, 30*24*time.Hour, fixedNow(18))

	if err := a.Archive("timetable", []byte("<html>502 Bad Gateway</html>")); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "2026-08-18.jsonl"))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	// The line as a whole must stay valid JSON, or the archive becomes
	// unreadable from the first bad response onwards.
	var rec entry
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("the archive line is not valid JSON: %v", err)
	}
	if !strings.Contains(string(rec.Payload), "502 Bad Gateway") {
		t.Errorf("payload = %s, want the raw body preserved", rec.Payload)
	}
}

func TestPruneRemovesOldFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]bool{
		"2026-08-18.jsonl": true,  // today
		"2026-07-20.jsonl": true,  // 29 days old
		"2026-07-01.jsonl": false, // 48 days old
		"2026-06-01.jsonl": false,
		"notes.txt":        true, // not ours to delete
		"garbled.jsonl":    true, // unparsable name, left alone
	}
	for name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	a := New(dir, 30*24*time.Hour, fixedNow(18))
	if err := a.Prune(); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	for name, wantKept := range files {
		_, err := os.Stat(filepath.Join(dir, name))
		if kept := err == nil; kept != wantKept {
			t.Errorf("%s kept = %v, want %v", name, kept, wantKept)
		}
	}
}

func TestPruneMissingDirectory(t *testing.T) {
	a := New(filepath.Join(t.TempDir(), "absent"), 30*24*time.Hour, fixedNow(18))
	if err := a.Prune(); err != nil {
		t.Errorf("pruning a directory that does not exist should be a no-op, got %v", err)
	}
}

// TestDisabledArchive checks an empty path turns the archiver into a no-op
// rather than an error, which is how -dry-run avoids leaving traces.
func TestDisabledArchive(t *testing.T) {
	a := New("", 30*24*time.Hour, fixedNow(18))
	if err := a.Archive("timetable", []byte("{}")); err != nil {
		t.Errorf("Archive: %v", err)
	}
	if err := a.Prune(); err != nil {
		t.Errorf("Prune: %v", err)
	}
}

func TestPruneWithoutRetention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2020-01-01.jsonl")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := New(dir, 0, fixedNow(18)).Prune(); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("with no retention configured nothing should be deleted")
	}
}
