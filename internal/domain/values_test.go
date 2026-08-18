package domain

import (
	"testing"
	"time"
)

// The String methods below appear in structured logs, which are the only
// record of why a given morning was decided the way it was. A mislabelled
// enum would make that record actively misleading, so the labels are pinned.

func TestCatchabilityString(t *testing.T) {
	tests := map[Catchability]string{
		Catchable: "CATCHABLE",
		Risky:     "RISKY",
		Missed:    "MISSED",
	}
	for c, want := range tests {
		if got := c.String(); got != want {
			t.Errorf("Catchability(%d).String() = %q, want %q", c, got, want)
		}
	}
}

func TestModeString(t *testing.T) {
	tests := map[Mode]string{
		ModeNormal:    "normal",
		ModeLate:      "late",
		ModeSevere:    "severe",
		ModeNoService: "no_service",
		ModeDegraded:  "degraded",
	}
	for m, want := range tests {
		if got := m.String(); got != want {
			t.Errorf("Mode(%d).String() = %q, want %q", m, got, want)
		}
	}
}

func TestDelaySourceString(t *testing.T) {
	if got := DelaySourceLive.String(); got != "live" {
		t.Errorf("live source = %q, want \"live\"", got)
	}
	if got := DelaySourceNone.String(); got != "none" {
		t.Errorf("absent source = %q, want \"none\"", got)
	}
}

func TestTickActionString(t *testing.T) {
	tests := map[TickAction]string{
		TickRun:    "run",
		TickGiveUp: "give_up",
		TickNone:   "none",
	}
	for a, want := range tests {
		if got := a.String(); got != want {
			t.Errorf("TickAction(%d).String() = %q, want %q", a, got, want)
		}
	}
}

func TestTimeOfDayString(t *testing.T) {
	if got := (TimeOfDay{Hour: 7, Minute: 50}).String(); got != "07:50" {
		t.Errorf("got %q, want \"07:50\"", got)
	}
	if got := (TimeOfDay{Hour: 23, Minute: 5}).String(); got != "23:05" {
		t.Errorf("got %q, want \"23:05\"", got)
	}
}

func TestParseUnknownTypePolicy(t *testing.T) {
	tests := []struct {
		in     string
		want   UnknownTypePolicy
		wantOK bool
	}{
		{"include_and_flag", IncludeAndFlag, true},
		{"exclude", ExcludeUnknown, true},
		{"", IncludeAndFlag, false},
		{"nonsense", IncludeAndFlag, false},
	}
	for _, tc := range tests {
		got, ok := ParseUnknownTypePolicy(tc.in)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("ParseUnknownTypePolicy(%q) = (%v, %v), want (%v, %v)",
				tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
	// An unrecognised value must fall back to the safer-by-design policy
	// rather than silently dropping trains.
	if got, _ := ParseUnknownTypePolicy("typo"); got != IncludeAndFlag {
		t.Errorf("fallback = %v, want include_and_flag", got)
	}
}

func TestSlackForNeverNegative(t *testing.T) {
	p := testParams()
	// Arriving well past the deadline: the slack is zero, not negative, so
	// the renderer never prints "餘裕 -12 分".
	if got := p.SlackFor(at("09:45")); got != 0 {
		t.Errorf("slack = %v, want 0 when already late", got)
	}
	if got := p.SlackFor(at("09:00")); got != 10*time.Minute {
		t.Errorf("slack = %v, want 10m", got)
	}
}

func TestCandidateMinuteAccessors(t *testing.T) {
	c := Candidate{Delay: 7 * time.Minute, Lateness: 3 * time.Minute}
	if got := c.DelayMinutes(); got != 7 {
		t.Errorf("delay = %d, want 7", got)
	}
	if got := c.LatenessMinutes(); got != 3 {
		t.Errorf("lateness = %d, want 3", got)
	}
	// Seconds are truncated, not rounded: TDX publishes whole minutes, and
	// inventing a rounding rule would only obscure that.
	c = Candidate{Delay: 90 * time.Second}
	if got := c.DelayMinutes(); got != 1 {
		t.Errorf("delay = %d, want 1", got)
	}
}

func TestBestCompensationEmpty(t *testing.T) {
	if got := (Brief{}).BestCompensation(); got != nil {
		t.Errorf("BestCompensation = %v, want nil with no options", got)
	}
}
