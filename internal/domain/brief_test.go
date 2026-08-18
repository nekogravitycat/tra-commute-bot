package domain

import (
	"testing"
	"time"
)

func testBriefInput(plan Plan) BriefInput {
	return BriefInput{
		GeneratedAt:         at("07:50"),
		Schedule:            "平日通勤",
		Route:               Route{OriginName: "桃園", DestinationName: "臺北"},
		Params:              testParams(),
		Plan:                plan,
		LiveDataAvailable:   true,
		CertificateEnabled:  true,
		CertificateMinDelay: 5 * time.Minute,
		CompensationEnabled: true,
		MaxEarlyLeave:       15 * time.Minute,
		SevereThreshold:     30 * time.Minute,
	}
}

// TestBriefModes pins which template each morning produces. The mode is derived
// rather than set, so the renderer and these tests cannot drift apart.
func TestBriefModes(t *testing.T) {
	tests := []struct {
		name     string
		delays   map[string]int
		lastMile time.Duration
		want     Mode
	}{
		{
			name:   "on time",
			delays: map[string]int{"1136": 0, "2008": 0, "1138": 0},
			want:   ModeNormal,
		},
		{
			name:   "late but manageable",
			delays: map[string]int{"1136": 0, "2008": 20, "1138": 20},
			want:   ModeLate,
		},
		{
			name:   "severely late",
			delays: map[string]int{"1136": 60, "2008": 60, "1138": 60},
			want:   ModeSevere,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := testBriefInput(buildTestPlan(t, usualServices(), tc.delays))
			if got := BuildBrief(in).Mode; got != tc.want {
				t.Errorf("mode = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBriefSevereBoundary pins the escalation threshold, which decides whether
// the message tells the user to contact their manager.
func TestBriefSevereBoundary(t *testing.T) {
	// Arrive so that lateness lands exactly on, then one minute past, the
	// 30 minute threshold.
	tests := []struct {
		arrival string
		want    Mode
	}{
		{"09:40", ModeLate},   // 30 minutes late: on the threshold, not past it
		{"09:41", ModeSevere}, // 31 minutes: past it
	}
	for _, tc := range tests {
		services := []Service{svc("9100", "1131", "區間", "08:30", tc.arrival)}
		in := testBriefInput(buildTestPlan(t, services, map[string]int{"9100": 0}))
		if got := BuildBrief(in).Mode; got != tc.want {
			t.Errorf("arriving %s: mode = %v, want %v", tc.arrival, got, tc.want)
		}
	}
}

// TestBriefNoServiceDoesNotPanic covers the degenerate morning: the window held
// nothing boardable. It must still produce a sendable brief.
func TestBriefNoServiceDoesNotPanic(t *testing.T) {
	b := BuildBrief(testBriefInput(buildTestPlan(t, nil, map[string]int{})))

	if b.Mode != ModeNoService {
		t.Errorf("mode = %v, want no service", b.Mode)
	}
	if b.Plan.Recommended != nil {
		t.Error("nothing should be recommended")
	}
	if b.BestCompensation() != nil {
		t.Error("no compensation is possible with no trains")
	}
}

// TestBriefSkipsSearchesWhenPunctual checks the certificate and compensation
// searches stay out of a message that has nothing to apologise for.
func TestBriefSkipsSearchesWhenPunctual(t *testing.T) {
	b := BuildBrief(testBriefInput(
		buildTestPlan(t, usualServices(), map[string]int{"1136": 0, "2008": 0, "1138": 0})))

	if b.Mode != ModeNormal {
		t.Fatalf("mode = %v, want normal", b.Mode)
	}
	if len(b.Compensations) != 0 {
		t.Error("a punctual morning needs no compensation options")
	}
	if b.Certificate.Found {
		t.Error("a punctual morning needs no delay certificate")
	}
}

// TestBriefRespectsDisabledFeatures checks the config switches are honoured.
func TestBriefRespectsDisabledFeatures(t *testing.T) {
	in := testBriefInput(buildTestPlan(t, usualServices(), map[string]int{"1136": 8, "2008": 24, "1138": 2}))
	in.CertificateEnabled = false
	in.CompensationEnabled = false

	b := BuildBrief(in)
	if b.Mode != ModeLate {
		t.Fatalf("mode = %v, want late", b.Mode)
	}
	if b.Certificate.Found {
		t.Error("certificate search ran despite being disabled")
	}
	if len(b.Compensations) != 0 {
		t.Error("compensation search ran despite being disabled")
	}
}

// TestDegradedBriefAlwaysUsable checks the fallback carries enough to send.
// A missing notification is indistinguishable from a quiet morning, and that
// ambiguity is exactly what the system exists to remove.
func TestDegradedBriefAlwaysUsable(t *testing.T) {
	plan := buildTestPlan(t, usualServices(), map[string]int{})
	in := testBriefInput(plan)

	b := DegradedBrief(in, Degradation{
		Stage:           "live",
		Detail:          "TDX timeout",
		SchedulesUsable: true,
	})

	if b.Mode != ModeDegraded {
		t.Errorf("mode = %v, want degraded", b.Mode)
	}
	if b.LiveDataAvailable {
		t.Error("degraded brief must not claim live data")
	}
	if b.Degradation == nil || b.Degradation.Stage != "live" {
		t.Errorf("degradation = %+v, want the live stage recorded", b.Degradation)
	}
	if len(b.Plan.Candidates) == 0 {
		t.Error("scheduled times survived the failure and should still be sent")
	}
}
