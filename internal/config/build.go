package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// Defaults for values the file may omit. Anything with a sensible default is
// optional; anything that would make the program guess about the user's
// commute is required.
const (
	defaultTolerance      = 2 * time.Minute
	defaultRetryWindow    = 10 * time.Minute
	defaultLookback       = 30 * time.Minute
	defaultLookahead      = 60 * time.Minute
	defaultBoardBuffer    = 2 * time.Minute
	defaultRiskMargin     = 3 * time.Minute
	defaultCertMinDelay   = 5 * time.Minute
	defaultSevere         = 30 * time.Minute
	defaultInterval       = 1500 * time.Millisecond
	defaultTimeout        = 15 * time.Second
	defaultMaxAlternative = 4
	defaultRetainDays     = 30
	defaultTimezone       = "Asia/Taipei"
	defaultStatePath      = "/var/lib/tra-commute/state.json"
	defaultSettingsPath   = "/var/lib/tra-commute/settings.json"
	defaultArchiveDir     = "/var/lib/tra-commute/dumps"
)

// defaultKnownTypeKeywords lists the train types confirmed to accept electronic
// tickets. A type matching none of these is unknown rather than eligible, which
// is what triggers the include-and-flag warning.
var defaultKnownTypeKeywords = []string{"區間快", "區間", "自強", "莒光", "復興", "普快"}

// Build validates the parsed file and converts it into the runtime config.
func (f File) Build() (Config, error) {
	var c Config
	var errs []string

	tz := f.Output.Timezone
	if tz == "" {
		tz = defaultTimezone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("load timezone %q: %w", tz, err)
	}
	c.Location = loc

	c.SkipDates = f.SkipDates
	c.ExtraDates = f.ExtraDates
	for _, d := range f.SkipDates {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			errs = append(errs, fmt.Sprintf("skip_dates: %q must be yyyy-MM-dd", d))
		}
	}
	for _, d := range f.ExtraDates {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			errs = append(errs, fmt.Sprintf("extra_dates: %q must be yyyy-MM-dd", d))
		}
	}
	c.Tolerance = minutesOr(f.TickToleranceMinutes, defaultTolerance)
	c.RetryWindow = minutesOr(f.RetryWindowMinutes, defaultRetryWindow)

	c.UsualTrainNos = f.Route.UsualTrainNos

	c.Board = minutesOr(f.Constraints.BoardingBufferMinutes, defaultBoardBuffer)
	c.RiskMargin = minutesOr(f.Constraints.RiskMarginMinutes, defaultRiskMargin)

	c.Window = domain.Window{
		Lookback:  minutesOr(f.Route.LookbackMinutes, defaultLookback),
		Lookahead: minutesOr(f.Route.LookaheadMinutes, defaultLookahead),
	}

	policy, ok := domain.ParseUnknownTypePolicy(orDefault(f.Route.UnknownTrainTypePolicy, "include_and_flag"))
	if !ok {
		errs = append(errs, fmt.Sprintf("route.unknown_train_type_policy %q must be include_and_flag or exclude",
			f.Route.UnknownTrainTypePolicy))
	}
	excluded := make(map[string]bool, len(f.Route.ExcludedTrainTypeIDs))
	for _, id := range f.Route.ExcludedTrainTypeIDs {
		excluded[id] = true
	}
	known := f.Route.KnownTrainTypeKeywords
	if len(known) == 0 {
		known = defaultKnownTypeKeywords
	}
	c.Filter = domain.TypeFilter{
		ExcludedIDs:      excluded,
		ExcludedKeywords: f.Route.ExcludedTrainTypeKeywords,
		KnownKeywords:    known,
		Policy:           policy,
	}

	c.CertificateEnabled = f.Certificate.Enabled
	c.CertificateMinDelay = minutesOr(f.Certificate.MinDelayMinutes, defaultCertMinDelay)
	c.CertificateNote = f.Certificate.Note

	c.CompensationEnabled = f.Compensation.Enabled
	c.SevereThreshold = minutesOr(f.Compensation.SevereDelayThreshold, defaultSevere)

	c.RequestInterval = defaultInterval
	if f.API.RequestIntervalMs > 0 {
		c.RequestInterval = time.Duration(f.API.RequestIntervalMs) * time.Millisecond
	}
	c.RequestTimeout = defaultTimeout
	if f.API.TimeoutSeconds > 0 {
		c.RequestTimeout = time.Duration(f.API.TimeoutSeconds) * time.Second
	}

	c.MaxAlternatives = f.Output.MaxAlternatives
	if c.MaxAlternatives <= 0 {
		c.MaxAlternatives = defaultMaxAlternative
	}

	c.StatePath = orDefault(f.Storage.StatePath, defaultStatePath)
	c.SettingsPath = orDefault(f.Storage.SettingsPath, defaultSettingsPath)
	c.ArchiveDir = orDefault(f.Storage.ArchiveDir, defaultArchiveDir)
	retain := f.Storage.ArchiveRetainDays
	if retain == 0 {
		retain = defaultRetainDays
	}
	c.ArchiveRetention = time.Duration(retain) * 24 * time.Hour

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid config:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return c, nil
}

func minutes(n int) time.Duration { return time.Duration(n) * time.Minute }

func minutesOr(n int, fallback time.Duration) time.Duration {
	if n <= 0 {
		return fallback
	}
	return minutes(n)
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
