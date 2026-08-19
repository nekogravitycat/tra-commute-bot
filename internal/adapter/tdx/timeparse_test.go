package tdx

import (
	"testing"
	"time"
)

var testLoc = time.FixedZone("Asia/Taipei", 8*3600)

// TestParseClockBothFormats covers the inconsistency that has already caused
// one silent failure: the timetable publishes "12:43" while other endpoints
// publish "14:42:00". A parser that accepts only one of them reads zeros from
// the other and reports no delays at all.
func TestParseClockBothFormats(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"12:43", 12*time.Hour + 43*time.Minute},
		{"14:42:00", 14*time.Hour + 42*time.Minute},
		{"00:02", 2 * time.Minute},
		{"23:28", 23*time.Hour + 28*time.Minute},
		{"09:05:30", 9*time.Hour + 5*time.Minute + 30*time.Second},
	}
	for _, tc := range tests {
		got, err := parseClock(tc.in)
		if err != nil {
			t.Errorf("parseClock(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseClock(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}

	for _, bad := range []string{"", "8:26 AM", "0826", "not a time"} {
		if _, err := parseClock(bad); err == nil {
			t.Errorf("parseClock(%q) should have failed", bad)
		}
	}
}

// TestResolveClock checks a naive clock lands on the right date in the right
// zone. Getting the zone wrong here would shift every time in the brief by
// eight hours while still looking perfectly well-formed.
func TestResolveClock(t *testing.T) {
	day := time.Date(2026, 8, 18, 0, 0, 0, 0, testLoc)

	got, err := resolveClock(day, "08:26")
	if err != nil {
		t.Fatalf("resolveClock: %v", err)
	}
	want := time.Date(2026, 8, 18, 8, 26, 0, 0, testLoc)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
	if got.Location() != testLoc {
		t.Errorf("location = %v, want %v", got.Location(), testLoc)
	}
}

func TestParseDate(t *testing.T) {
	got, err := parseDate("2026-08-18", testLoc)
	if err != nil {
		t.Fatalf("parseDate: %v", err)
	}
	if got.Year() != 2026 || got.Month() != time.August || got.Day() != 18 {
		t.Errorf("got %s, want 2026-08-18", got)
	}
	// The API rejects slash-separated dates outright, so the parser has no
	// reason to accept them either.
	if _, err := parseDate("2026/08/18", testLoc); err == nil {
		t.Error("parseDate should reject yyyy/MM/dd")
	}
}

// TestParseTimestamp covers the other half of the format split: the live board
// carries a real RFC3339 timestamp with a zone, unlike the naive timetable
// clocks.
func TestParseTimestamp(t *testing.T) {
	got, err := parseTimestamp("2026-08-18T14:26:40+08:00", testLoc)
	if err != nil {
		t.Fatalf("parseTimestamp: %v", err)
	}
	want := time.Date(2026, 8, 18, 14, 26, 40, 0, testLoc)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}

	// An unparsable timestamp yields the zero value and an error — the
	// caller logs the error and treats the zero value as "unknown" metadata,
	// not a reason to abandon an otherwise good brief.
	got, err = parseTimestamp("garbage", testLoc)
	if err == nil {
		t.Error("expected an error for an unparsable timestamp")
	}
	if !got.IsZero() {
		t.Errorf("got %s, want the zero time", got)
	}
}
