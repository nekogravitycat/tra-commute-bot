package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// writeConfig puts a config file in a temp dir and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestLoadExampleConfig checks the shipped example is valid. It is the file
// users copy, so a stale key in it would break every fresh install.
func TestLoadExampleConfig(t *testing.T) {
	cfg, err := Load("../../configs/config.example.yaml")
	if err != nil {
		t.Fatalf("the shipped example config does not load: %v", err)
	}

	if cfg.Location.String() != "Asia/Taipei" {
		t.Errorf("timezone = %s, want Asia/Taipei", cfg.Location)
	}
	if cfg.OriginID != "1080" || cfg.DestID != "1000" {
		t.Errorf("route = %s → %s, want 1080 → 1000", cfg.OriginID, cfg.DestID)
	}
	if cfg.ReadyLead != 30*time.Minute {
		t.Errorf("ready lead = %v, want 30m", cfg.ReadyLead)
	}
	if cfg.Deadline != (domain.TimeOfDay{Hour: 9, Minute: 30}) {
		t.Errorf("deadline = %v, want 09:30", cfg.Deadline)
	}
	if cfg.LastMile != 20*time.Minute {
		t.Errorf("last mile = %v, want 20m", cfg.LastMile)
	}
	if len(cfg.Scheduling.Schedules) == 0 {
		t.Fatal("no schedules configured")
	}
	if got := cfg.Scheduling.Schedules[0].At; got != (domain.TimeOfDay{Hour: 7, Minute: 50}) {
		t.Errorf("schedule time = %v, want 07:50", got)
	}

	// The two train types that refuse electronic tickets must be excluded by
	// ID: the type code cannot distinguish them, since 自強 alone spans two.
	for _, id := range []string{"1101", "1107"} {
		if !cfg.Filter.ExcludedIDs[id] {
			t.Errorf("train type %s should be excluded", id)
		}
	}
	if cfg.Filter.Policy != domain.IncludeAndFlag {
		t.Errorf("unknown type policy = %v, want include_and_flag", cfg.Filter.Policy)
	}
}

// TestLocalConfigMatchesExample guards against the development config drifting
// away from the one that ships.
func TestLocalConfigMatchesExample(t *testing.T) {
	local, err := Load("../../configs/config.local.yaml")
	if err != nil {
		t.Fatalf("the local config does not load: %v", err)
	}
	example, err := Load("../../configs/config.example.yaml")
	if err != nil {
		t.Fatalf("the example config does not load: %v", err)
	}

	if local.Deadline != example.Deadline || local.ReadyLead != example.ReadyLead ||
		local.LastMile != example.LastMile || local.OriginID != example.OriginID {
		t.Error("the local config's commute parameters have drifted from the example")
	}
}

const minimalConfig = `
schedules:
  - name: "平日通勤"
    weekdays: [Mon, Fri]
    at: "07:50"
timing:
  ready_lead_minutes: 30
route:
  origin_station_id: "1080"
  destination_station_id: "1000"
constraints:
  clock_in_deadline: "09:30"
  last_mile_minutes: 20
`

// TestDefaults checks every optional value has a sensible fallback, so a short
// config is a valid one.
func TestDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, minimalConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"tolerance", cfg.Scheduling.Tolerance, 2 * time.Minute},
		{"retry window", cfg.Scheduling.RetryWindow, 10 * time.Minute},
		{"lookback", cfg.Window.Lookback, 30 * time.Minute},
		{"lookahead", cfg.Window.Lookahead, 60 * time.Minute},
		{"boarding buffer", cfg.Board, 2 * time.Minute},
		{"risk margin", cfg.RiskMargin, 3 * time.Minute},
		{"certificate minimum", cfg.CertificateMinDelay, 5 * time.Minute},
		{"max early leave", cfg.MaxEarlyLeave, 15 * time.Minute},
		{"severe threshold", cfg.SevereThreshold, 30 * time.Minute},
		{"request interval", cfg.RequestInterval, 1500 * time.Millisecond},
		{"request timeout", cfg.RequestTimeout, 15 * time.Second},
		{"max alternatives", cfg.MaxAlternatives, 4},
		{"archive retention", cfg.ArchiveRetention, 30 * 24 * time.Hour},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	if cfg.Location.String() != "Asia/Taipei" {
		t.Errorf("timezone = %s, want Asia/Taipei", cfg.Location)
	}
	// The route names fall back to the station IDs rather than being blank.
	if cfg.Route.OriginName != "1080" {
		t.Errorf("origin name = %q, want the station ID as a fallback", cfg.Route.OriginName)
	}
	if len(cfg.Filter.KnownKeywords) == 0 {
		t.Error("the default set of known train types should not be empty")
	}
}

func TestWeekdayParsing(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
schedules:
  - name: "test"
    weekdays: [Mon, tuesday, WED, thu, Fri, sat, SUN]
    at: "07:50"
timing:
  ready_lead_minutes: 30
route:
  origin_station_id: "1080"
  destination_station_id: "1000"
constraints:
  clock_in_deadline: "09:30"
  last_mile_minutes: 20
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := cfg.Scheduling.Schedules[0].Weekdays
	want := []time.Weekday{
		time.Monday, time.Tuesday, time.Wednesday, time.Thursday,
		time.Friday, time.Saturday, time.Sunday,
	}
	if len(got) != len(want) {
		t.Fatalf("weekdays = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("weekday %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		config    string
		wantError string
	}{
		{
			name:      "no schedules",
			config:    "timing:\n  ready_lead_minutes: 30\n",
			wantError: "schedules",
		},
		{
			name: "schedule with neither weekdays nor dates",
			config: `
schedules:
  - name: "empty"
    at: "07:50"
timing:
  ready_lead_minutes: 30
route:
  origin_station_id: "1080"
  destination_station_id: "1000"
constraints:
  clock_in_deadline: "09:30"
  last_mile_minutes: 20
`,
			wantError: "weekdays or dates",
		},
		{
			name: "duplicate schedule names",
			config: `
schedules:
  - name: "same"
    weekdays: [Mon]
    at: "07:50"
  - name: "same"
    weekdays: [Tue]
    at: "07:50"
timing:
  ready_lead_minutes: 30
route:
  origin_station_id: "1080"
  destination_station_id: "1000"
constraints:
  clock_in_deadline: "09:30"
  last_mile_minutes: 20
`,
			wantError: "duplicate name",
		},
		{
			name: "bad weekday",
			config: `
schedules:
  - name: "test"
    weekdays: [Mondayish]
    at: "07:50"
timing:
  ready_lead_minutes: 30
route:
  origin_station_id: "1080"
  destination_station_id: "1000"
constraints:
  clock_in_deadline: "09:30"
  last_mile_minutes: 20
`,
			wantError: "unknown weekday",
		},
		{
			name: "bad date format",
			config: `
schedules:
  - name: "test"
    dates: ["2026/09/26"]
    at: "07:50"
timing:
  ready_lead_minutes: 30
route:
  origin_station_id: "1080"
  destination_station_id: "1000"
constraints:
  clock_in_deadline: "09:30"
  last_mile_minutes: 20
`,
			wantError: "yyyy-MM-dd",
		},
		{
			name: "bad skip date",
			config: `
schedules:
  - name: "test"
    weekdays: [Mon]
    at: "07:50"
skip_dates: ["tomorrow"]
timing:
  ready_lead_minutes: 30
route:
  origin_station_id: "1080"
  destination_station_id: "1000"
constraints:
  clock_in_deadline: "09:30"
  last_mile_minutes: 20
`,
			wantError: "skip_dates",
		},
		{
			name: "missing station",
			config: `
schedules:
  - name: "test"
    weekdays: [Mon]
    at: "07:50"
timing:
  ready_lead_minutes: 30
route:
  origin_station_id: "1080"
constraints:
  clock_in_deadline: "09:30"
  last_mile_minutes: 20
`,
			wantError: "destination_station_id",
		},
		{
			name: "missing deadline",
			config: `
schedules:
  - name: "test"
    weekdays: [Mon]
    at: "07:50"
timing:
  ready_lead_minutes: 30
route:
  origin_station_id: "1080"
  destination_station_id: "1000"
constraints:
  last_mile_minutes: 20
`,
			wantError: "clock_in_deadline",
		},
		{
			name: "zero ready lead",
			config: `
schedules:
  - name: "test"
    weekdays: [Mon]
    at: "07:50"
route:
  origin_station_id: "1080"
  destination_station_id: "1000"
constraints:
  clock_in_deadline: "09:30"
  last_mile_minutes: 20
`,
			wantError: "ready_lead_minutes",
		},
		{
			name: "bad policy",
			config: `
schedules:
  - name: "test"
    weekdays: [Mon]
    at: "07:50"
timing:
  ready_lead_minutes: 30
route:
  origin_station_id: "1080"
  destination_station_id: "1000"
  unknown_train_type_policy: "maybe"
constraints:
  clock_in_deadline: "09:30"
  last_mile_minutes: 20
`,
			wantError: "unknown_train_type_policy",
		},
		{
			name: "bad timezone",
			config: `
schedules:
  - name: "test"
    weekdays: [Mon]
    at: "07:50"
timing:
  ready_lead_minutes: 30
route:
  origin_station_id: "1080"
  destination_station_id: "1000"
constraints:
  clock_in_deadline: "09:30"
  last_mile_minutes: 20
output:
  timezone: "Mars/Olympus_Mons"
`,
			wantError: "timezone",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.config))
			if err == nil {
				t.Fatalf("expected an error mentioning %q", tc.wantError)
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Errorf("error = %q, should mention %q", err, tc.wantError)
			}
		})
	}
}

// TestUnknownKeyRejected checks a typo is reported rather than silently
// ignored. A misspelled key that quietly reverts to a default is exactly the
// kind of invisible wrongness this program exists to avoid.
func TestUnknownKeyRejected(t *testing.T) {
	_, err := Load(writeConfig(t, minimalConfig+"\nlast_mile_minutes: 20\n"))
	if err == nil {
		t.Fatal("a misplaced key should be rejected")
	}
	if !strings.Contains(err.Error(), "last_mile_minutes") {
		t.Errorf("error = %q, should name the offending key", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Error("expected an error for a missing config file")
	}
}

func TestLoadMalformedYAML(t *testing.T) {
	if _, err := Load(writeConfig(t, "schedules: [oops\n")); err == nil {
		t.Error("expected a parse error")
	}
}
