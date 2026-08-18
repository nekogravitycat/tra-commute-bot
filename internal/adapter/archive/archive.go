// Package archive stores the raw API responses of each run.
//
// When a recommendation turns out to have been wrong, this is the only way to
// answer why: the live delay figures that produced it are gone from TDX within
// minutes. The cost is a few hundred kilobytes a month.
package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Dir writes one file per run date, holding every response body captured that
// day, and prunes anything older than Retention.
type Dir struct {
	Path      string
	Retention time.Duration
	Now       func() time.Time
}

// New builds an archiver rooted at path.
func New(path string, retention time.Duration, now func() time.Time) *Dir {
	if now == nil {
		now = time.Now
	}
	return &Dir{Path: path, Retention: retention, Now: now}
}

type entry struct {
	At      time.Time       `json:"at"`
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

// Archive appends one raw response. Failures are returned rather than swallowed
// so the caller can log them, but no caller should ever treat them as fatal:
// losing a diagnostic copy is not a reason to withhold today's brief.
func (d *Dir) Archive(name string, payload []byte) error {
	if d.Path == "" {
		return nil
	}
	if err := os.MkdirAll(d.Path, 0o755); err != nil {
		return fmt.Errorf("create archive dir: %w", err)
	}

	now := d.Now()
	rec := entry{At: now, Name: name}
	if json.Valid(payload) {
		rec.Payload = payload
	} else {
		// Keep unparsable bodies too; an HTML error page from a proxy is
		// exactly the kind of thing worth having a copy of.
		quoted, _ := json.Marshal(string(payload))
		rec.Payload = quoted
	}

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode archive entry: %w", err)
	}

	file := filepath.Join(d.Path, now.Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}
	return nil
}

// Prune deletes archives older than the retention period.
func (d *Dir) Prune() error {
	if d.Path == "" || d.Retention <= 0 {
		return nil
	}
	entries, err := os.ReadDir(d.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read archive dir: %w", err)
	}

	cutoff := d.Now().Add(-d.Retention)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		// The date is taken from the filename rather than the modification
		// time, which survives a copy or a restore.
		day, err := time.Parse("2006-01-02", strings.TrimSuffix(e.Name(), ".jsonl"))
		if err != nil {
			continue
		}
		if day.Before(cutoff) {
			_ = os.Remove(filepath.Join(d.Path, e.Name()))
		}
	}
	return nil
}
