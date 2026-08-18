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
	defaultMaxEarlyLeave  = 15 * time.Minute
	defaultSevere         = 30 * time.Minute
	defaultInterval       = 1500 * time.Millisecond
	defaultTimeout        = 15 * time.Second
	defaultMaxAlternative = 4
	defaultRetainDays     = 30
	defaultTimezone       = "Asia/Taipei"
	defaultStatePath      = "/var/lib/tra-commute/state.json"
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

	c.Scheduling, err = f.scheduling()
	if err != nil {
		errs = append(errs, err.Error())
	}

	if f.Route.OriginStationID == "" {
		errs = append(errs, "route.origin_station_id is required")
	}
	if f.Route.DestinationStationID == "" {
		errs = append(errs, "route.destination_station_id is required")
	}
	c.OriginID = f.Route.OriginStationID
	c.DestID = f.Route.DestinationStationID
	c.Route = domain.Route{
		OriginName:      orDefault(f.Route.OriginName, f.Route.OriginStationID),
		DestinationName: orDefault(f.Route.DestinationName, f.Route.DestinationStationID),
	}
	c.UsualTrainNos = f.Route.UsualTrainNos

	if f.Timing.ReadyLeadMinutes <= 0 {
		errs = append(errs, "timing.ready_lead_minutes must be positive")
	}
	c.ReadyLead = minutes(f.Timing.ReadyLeadMinutes)

	if f.Constraints.ClockInDeadline == "" {
		errs = append(errs, "constraints.clock_in_deadline is required")
	} else {
		d, err := domain.ParseTimeOfDay(f.Constraints.ClockInDeadline)
		if err != nil {
			errs = append(errs, "constraints.clock_in_deadline: "+err.Error())
		}
		c.Deadline = d
	}
	if f.Constraints.LastMileMinutes < 0 {
		errs = append(errs, "constraints.last_mile_minutes must not be negative")
	}
	c.LastMile = minutes(f.Constraints.LastMileMinutes)
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
	c.MaxEarlyLeave = minutesOr(f.Compensation.MaxEarlyLeaveMinutes, defaultMaxEarlyLeave)
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

func (f File) scheduling() (domain.Scheduling, error) {
	s := domain.Scheduling{
		SkipDates:   f.SkipDates,
		Tolerance:   minutesOr(f.TickToleranceMinutes, defaultTolerance),
		RetryWindow: minutesOr(f.RetryWindowMinutes, defaultRetryWindow),
	}
	if len(f.Schedules) == 0 {
		return s, fmt.Errorf("schedules must contain at least one entry")
	}

	seen := map[string]bool{}
	for i, sf := range f.Schedules {
		name := sf.Name
		if name == "" {
			name = fmt.Sprintf("schedule-%d", i+1)
		}
		// Names key the state file, so duplicates would let one schedule
		// silently mark another as already delivered.
		if seen[name] {
			return s, fmt.Errorf("schedules[%d]: duplicate name %q", i, name)
		}
		seen[name] = true

		at, err := domain.ParseTimeOfDay(sf.At)
		if err != nil {
			return s, fmt.Errorf("schedules[%d] (%s): %w", i, name, err)
		}
		if len(sf.Weekdays) == 0 && len(sf.Dates) == 0 {
			return s, fmt.Errorf("schedules[%d] (%s): needs weekdays or dates", i, name)
		}

		sch := domain.Schedule{Name: name, At: at, Dates: sf.Dates}
		for _, w := range sf.Weekdays {
			wd, err := parseWeekday(w)
			if err != nil {
				return s, fmt.Errorf("schedules[%d] (%s): %w", i, name, err)
			}
			sch.Weekdays = append(sch.Weekdays, wd)
		}
		for _, d := range sf.Dates {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return s, fmt.Errorf("schedules[%d] (%s): date %q must be yyyy-MM-dd", i, name, d)
			}
		}
		s.Schedules = append(s.Schedules, sch)
	}
	for _, d := range f.SkipDates {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return s, fmt.Errorf("skip_dates: %q must be yyyy-MM-dd", d)
		}
	}
	return s, nil
}

var weekdayNames = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday,
	"mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday,
	"wed": time.Wednesday, "weds": time.Wednesday, "wednesday": time.Wednesday,
	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
	"fri": time.Friday, "friday": time.Friday,
	"sat": time.Saturday, "saturday": time.Saturday,
}

func parseWeekday(s string) (time.Weekday, error) {
	if wd, ok := weekdayNames[strings.ToLower(strings.TrimSpace(s))]; ok {
		return wd, nil
	}
	return 0, fmt.Errorf("unknown weekday %q", s)
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
