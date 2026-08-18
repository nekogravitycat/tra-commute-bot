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
type File struct {
	Schedules            []scheduleFile `yaml:"schedules"`
	SkipDates            []string       `yaml:"skip_dates"`
	TickToleranceMinutes int            `yaml:"tick_tolerance_minutes"`
	RetryWindowMinutes   int            `yaml:"retry_window_minutes"`

	Timing struct {
		ReadyLeadMinutes int `yaml:"ready_lead_minutes"`
	} `yaml:"timing"`

	Route struct {
		OriginStationID      string   `yaml:"origin_station_id"`
		OriginName           string   `yaml:"origin_name"`
		DestinationStationID string   `yaml:"destination_station_id"`
		DestinationName      string   `yaml:"destination_name"`
		UsualTrainNos        []string `yaml:"usual_train_nos"`
		LookbackMinutes      int      `yaml:"lookback_minutes"`
		LookaheadMinutes     int      `yaml:"lookahead_minutes"`

		ExcludedTrainTypeIDs      []string `yaml:"excluded_train_type_ids"`
		ExcludedTrainTypeKeywords []string `yaml:"excluded_train_type_keywords"`
		KnownTrainTypeKeywords    []string `yaml:"known_train_type_keywords"`
		UnknownTrainTypePolicy    string   `yaml:"unknown_train_type_policy"`
	} `yaml:"route"`

	Constraints struct {
		ClockInDeadline       string `yaml:"clock_in_deadline"`
		LastMileMinutes       int    `yaml:"last_mile_minutes"`
		BoardingBufferMinutes int    `yaml:"boarding_buffer_minutes"`
		RiskMarginMinutes     int    `yaml:"risk_margin_minutes"`
	} `yaml:"constraints"`

	Certificate struct {
		Enabled         bool   `yaml:"enabled"`
		MinDelayMinutes int    `yaml:"min_delay_minutes"`
		Note            string `yaml:"note"`
	} `yaml:"certificate"`

	Compensation struct {
		Enabled              bool `yaml:"enabled"`
		MaxEarlyLeaveMinutes int  `yaml:"max_early_leave_minutes"`
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
		ArchiveDir        string `yaml:"archive_dir"`
		ArchiveRetainDays int    `yaml:"archive_retain_days"`
	} `yaml:"storage"`
}

type scheduleFile struct {
	Name     string   `yaml:"name"`
	Weekdays []string `yaml:"weekdays"`
	Dates    []string `yaml:"dates"`
	At       string   `yaml:"at"`
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
	Location   *time.Location
	Scheduling domain.Scheduling

	OriginID string
	DestID   string
	Route    domain.Route

	ReadyLead  time.Duration
	Deadline   domain.TimeOfDay
	LastMile   time.Duration
	Board      time.Duration
	RiskMargin time.Duration

	Window        domain.Window
	Filter        domain.TypeFilter
	UsualTrainNos []string

	CertificateEnabled  bool
	CertificateMinDelay time.Duration
	CertificateNote     string

	CompensationEnabled bool
	MaxEarlyLeave       time.Duration
	SevereThreshold     time.Duration

	RequestInterval time.Duration
	RequestTimeout  time.Duration

	MaxAlternatives int

	StatePath        string
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
