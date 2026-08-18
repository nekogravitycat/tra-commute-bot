// Package config loads and validates the YAML configuration and the
// environment-supplied credentials, then converts them into the domain and
// use-case types the rest of the program works with.
//
// It lives in the outer layer on purpose: the domain must not know that any of
// its parameters came from a file, and the conversion happens once here rather
// than being rediscovered at each call site.
package config

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// File mirrors the YAML document. Field names match the specification's
// config.yaml exactly, so the file and this struct can be read side by side.
//
// This holds only the calibration knobs an admin edits by hand: the six trip
// parameters that change often (schedule, ready time, deadline, route, max
// early leave) live in the settings file instead, set live over Telegram —
// see internal/domain.Settings and internal/adapter/settingsfile.
type File struct {
	// ExtraDates are one-off make-up workdays (補班日) that fire on the same
	// schedule time as the regular weekday schedule. Rare enough that a
	// manual config edit is the right interface for them.
	ExtraDates           []string `yaml:"extra_dates"`
	SkipDates            []string `yaml:"skip_dates"`
	TickToleranceMinutes int      `yaml:"tick_tolerance_minutes"`
	RetryWindowMinutes   int      `yaml:"retry_window_minutes"`

	Route struct {
		UsualTrainNos    []string `yaml:"usual_train_nos"`
		LookbackMinutes  int      `yaml:"lookback_minutes"`
		LookaheadMinutes int      `yaml:"lookahead_minutes"`

		ExcludedTrainTypeIDs      []string `yaml:"excluded_train_type_ids"`
		ExcludedTrainTypeKeywords []string `yaml:"excluded_train_type_keywords"`
		KnownTrainTypeKeywords    []string `yaml:"known_train_type_keywords"`
		UnknownTrainTypePolicy    string   `yaml:"unknown_train_type_policy"`
	} `yaml:"route"`

	Constraints struct {
		BoardingBufferMinutes int `yaml:"boarding_buffer_minutes"`
		RiskMarginMinutes     int `yaml:"risk_margin_minutes"`
	} `yaml:"constraints"`

	Certificate struct {
		Enabled         bool   `yaml:"enabled"`
		MinDelayMinutes int    `yaml:"min_delay_minutes"`
		Note            string `yaml:"note"`
	} `yaml:"certificate"`

	Compensation struct {
		Enabled              bool `yaml:"enabled"`
		SevereDelayThreshold int  `yaml:"severe_delay_threshold"`
	} `yaml:"compensation"`

	API struct {
		RequestIntervalMs int `yaml:"request_interval_ms"`
		TimeoutSeconds    int `yaml:"timeout_seconds"`
	} `yaml:"api"`

	Output struct {
		MaxAlternatives int    `yaml:"max_alternatives"`
		Timezone        string `yaml:"timezone"`
	} `yaml:"output"`

	Storage struct {
		StatePath         string `yaml:"state_path"`
		SettingsPath      string `yaml:"settings_path"`
		ArchiveDir        string `yaml:"archive_dir"`
		ArchiveRetainDays int    `yaml:"archive_retain_days"`
	} `yaml:"storage"`
}

// Credentials come from the environment, never from the config file, so the
// file itself can be world-readable and kept in version control.
type Credentials struct {
	TDXClientID     string
	TDXClientSecret string
	TelegramToken   string
	TelegramChatID  string
}

// Config is the validated, converted result.
type Config struct {
	Location *time.Location

	// SkipDates, ExtraDates, Tolerance and RetryWindow are the guard
	// parameters that stay admin-only; the schedule's own weekdays and fire
	// time come from the live settings file instead (usecase.Tick merges
	// the two into a domain.Scheduling on every tick).
	SkipDates   []string
	ExtraDates  []string
	Tolerance   time.Duration
	RetryWindow time.Duration

	Board      time.Duration
	RiskMargin time.Duration

	Window        domain.Window
	Filter        domain.TypeFilter
	UsualTrainNos []string

	CertificateEnabled  bool
	CertificateMinDelay time.Duration
	CertificateNote     string

	CompensationEnabled bool
	SevereThreshold     time.Duration

	RequestInterval time.Duration
	RequestTimeout  time.Duration

	MaxAlternatives int

	StatePath        string
	SettingsPath     string
	ArchiveDir       string
	ArchiveRetention time.Duration

	Credentials Credentials
}

// Load reads, validates and converts the config file at path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var f File
	// KnownFields would be stricter, but yaml.v3 only offers it through a
	// decoder, and a config that silently ignores a typo'd key is exactly the
	// kind of quiet wrongness this program is trying to avoid.
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return f.Build()
}
