package tdx

import (
	"fmt"
	"time"
)

// TDX is not internally consistent about clock formats: the daily timetable
// publishes "08:26" while other endpoints publish "14:42:00", and the field
// names differ too. Accepting both here, once, is cheaper than discovering the
// mismatch at runtime in a caller that only ever expected one of them.
var clockLayouts = []string{"15:04", "15:04:05"}

// parseClock reads a naive clock string in either published format and returns
// the offset from midnight.
func parseClock(s string) (time.Duration, error) {
	for _, layout := range clockLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Duration(t.Hour())*time.Hour +
				time.Duration(t.Minute())*time.Minute +
				time.Duration(t.Second())*time.Second, nil
		}
	}
	return 0, fmt.Errorf("unrecognised clock %q, want HH:mm or HH:mm:ss", s)
}

// resolveClock anchors a naive clock string to a service date.
func resolveClock(day time.Time, s string) (time.Time, error) {
	d, err := parseClock(s)
	if err != nil {
		return time.Time{}, err
	}
	midnight := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	return midnight.Add(d), nil
}

// parseDate reads TDX's yyyy-MM-dd service date. yyyy/MM/dd is rejected by the
// API itself, so only the one layout is accepted here.
func parseDate(s string, loc *time.Location) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", s, loc)
}

// parseTimestamp reads the RFC3339 timestamps that the live board carries.
// These do include a zone, unlike the timetable's naive clocks; mixing the two
// up is the single easiest mistake to make against this API.
func parseTimestamp(s string, loc *time.Location) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.In(loc)
}
