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
	if cfg.Tolerance != 2*time.Minute {
		t.Errorf("tolerance = %v, want 2m", cfg.Tolerance)
	}
	if cfg.RetryWindow != 10*time.Minute {
		t.Errorf("retry window = %v, want 10m", cfg.RetryWindow)
	}
	if got := cfg.UsualTrainNos; len(got) != 3 {
		t.Errorf("usual train nos = %v, want 3 entries", got)
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
	if cfg.SettingsPath == "" {
		t.Error("settings_path should default to a non-empty path")
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

	if local.Board != example.Board || local.RiskMargin != example.RiskMargin ||
		local.Tolerance != example.Tolerance || local.RetryWindow != example.RetryWindow {
		t.Error("the local config's calibration knobs have drifted from the example")
	}
}

const minimalConfig = `
route:
  usual_train_nos: []
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
		{"tolerance", cfg.Tolerance, 2 * time.Minute},
		{"retry window", cfg.RetryWindow, 10 * time.Minute},
		{"lookback", cfg.Window.Lookback, 30 * time.Minute},
		{"lookahead", cfg.Window.Lookahead, 60 * time.Minute},
		{"boarding buffer", cfg.Board, 2 * time.Minute},
		{"risk margin", cfg.RiskMargin, 3 * time.Minute},
		{"certificate minimum", cfg.CertificateMinDelay, 5 * time.Minute},
		{"severe threshold", cfg.SevereThreshold, 30 * time.Minute},
		{"request interval", cfg.RequestInterval, 1500 * time.Millisecond},
		{"request timeout", cfg.RequestTimeout, 15 * time.Second},
		{"max alternatives", cfg.MaxAlternatives, 4},
		{"archive retention", cfg.ArchiveRetention, 30 * 24 * time.Hour},
		{"state path", cfg.StatePath, "/var/lib/tra-commute/state.json"},
		{"settings path", cfg.SettingsPath, "/var/lib/tra-commute/settings.json"},
		{"archive dir", cfg.ArchiveDir, "/var/lib/tra-commute/dumps"},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	if cfg.Location.String() != "Asia/Taipei" {
		t.Errorf("timezone = %s, want Asia/Taipei", cfg.Location)
	}
	if len(cfg.Filter.KnownKeywords) == 0 {
		t.Error("the default set of known train types should not be empty")
	}
}

func TestSkipAndExtraDates(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
skip_dates: ["2026-09-01"]
extra_dates: ["2026-09-26"]
route:
  usual_train_nos: []
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.SkipDates) != 1 || cfg.SkipDates[0] != "2026-09-01" {
		t.Errorf("skip dates = %v, want [2026-09-01]", cfg.SkipDates)
	}
	if len(cfg.ExtraDates) != 1 || cfg.ExtraDates[0] != "2026-09-26" {
		t.Errorf("extra dates = %v, want [2026-09-26]", cfg.ExtraDates)
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		config    string
		wantError string
	}{
		{
			name: "bad skip date",
			config: `
skip_dates: ["tomorrow"]
route:
  usual_train_nos: []
`,
			wantError: "skip_dates",
		},
		{
			name: "bad extra date",
			config: `
extra_dates: ["tomorrow"]
route:
  usual_train_nos: []
`,
			wantError: "extra_dates",
		},
		{
			name: "bad policy",
			config: `
route:
  usual_train_nos: []
  unknown_train_type_policy: "maybe"
`,
			wantError: "unknown_train_type_policy",
		},
		{
			name: "bad timezone",
			config: `
route:
  usual_train_nos: []
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
	if _, err := Load(writeConfig(t, "route: [oops\n")); err == nil {
		t.Error("expected a parse error")
	}
}
