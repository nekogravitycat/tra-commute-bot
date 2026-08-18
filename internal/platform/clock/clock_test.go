package clock

import (
	"testing"
	"time"
)

func TestReal(t *testing.T) {
	loc := time.FixedZone("Asia/Taipei", 8*3600)
	got := Real{Loc: loc}.Now()

	// The location is explicit rather than local, so the program behaves the
	// same on a laptop as on a server whose system zone is UTC.
	if got.Location() != loc {
		t.Errorf("location = %v, want %v", got.Location(), loc)
	}
	if time.Since(got) > time.Minute {
		t.Errorf("now = %s, which is not now", got)
	}
}

func TestFixed(t *testing.T) {
	want := time.Date(2026, 8, 18, 7, 50, 0, 0, time.UTC)
	c := Fixed{At: want}

	// Repeated reads must agree: a simulated run that saw time move would be
	// no more reproducible than a real one.
	for i := 0; i < 3; i++ {
		if got := c.Now(); !got.Equal(want) {
			t.Errorf("read %d = %s, want %s", i, got, want)
		}
	}
}
