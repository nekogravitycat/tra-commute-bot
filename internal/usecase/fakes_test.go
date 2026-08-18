package usecase

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

var testLoc = time.FixedZone("Asia/Taipei", 8*3600)

func at(hhmm string) time.Time {
	t, err := time.ParseInLocation("15:04", hhmm, testLoc)
	if err != nil {
		panic(err)
	}
	return time.Date(2026, 8, 18, t.Hour(), t.Minute(), 0, 0, testLoc)
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// errBoom stands in for any transport failure.
var errBoom = errors.New("boom")

type fakeTimetable struct {
	services []domain.Service
	err      error
	calls    int
}

func (f *fakeTimetable) DailyODTimetable(_ context.Context, _, _ string, date time.Time) (Timetable, error) {
	f.calls++
	if f.err != nil {
		return Timetable{}, f.err
	}
	return Timetable{ServiceDate: date, UpdatedAt: at("07:49"), Services: f.services}, nil
}

type fakeDelays struct {
	delays map[string]int
	err    error
	calls  int
}

func (f *fakeDelays) LiveDelays(context.Context) (DelaySnapshot, error) {
	f.calls++
	if f.err != nil {
		return DelaySnapshot{}, f.err
	}
	return DelaySnapshot{UpdatedAt: at("07:49"), ByTrainNo: f.delays}, nil
}

// fakeRenderer records the brief it was handed, so the tests can assert on the
// decision rather than on the wording of the message.
type fakeRenderer struct{ last domain.Brief }

func (f *fakeRenderer) Render(b domain.Brief) Message {
	f.last = b
	return Message{Text: "rendered:" + b.Mode.String(), ParseMode: "HTML"}
}

// fakeNotifier fails its first failUntil sends, then succeeds.
type fakeNotifier struct {
	sent     []Message
	failFor  int
	attempts int
}

func (f *fakeNotifier) Send(_ context.Context, m Message) error {
	f.attempts++
	if f.attempts <= f.failFor {
		return errBoom
	}
	f.sent = append(f.sent, m)
	return nil
}

type fakeState struct {
	state    domain.TickState
	loadErr  error
	saveErr  error
	saves    int
	lastSave domain.TickState
}

func (f *fakeState) Load() (domain.TickState, error) {
	if f.loadErr != nil {
		return domain.TickState{}, f.loadErr
	}
	return f.state, nil
}

func (f *fakeState) Save(s domain.TickState) error {
	f.saves++
	f.lastSave = s
	if f.saveErr != nil {
		return f.saveErr
	}
	f.state = s
	return nil
}

// fakeSettings backs SettingsStore. It defaults to a single-schedule list
// built from testTrip() when no settings have been explicitly loaded, so
// tests that do not care about the live-settings plumbing keep working
// against a complete trip.
type fakeSettings struct {
	settings *domain.SettingsList // nil until Save or an explicit preset is given
	loadErr  error
	saveErr  error
	saves    int
}

func (f *fakeSettings) Load() (domain.SettingsList, error) {
	if f.loadErr != nil {
		return domain.SettingsList{}, f.loadErr
	}
	if f.settings == nil {
		return domain.SettingsList{Schedules: []domain.Settings{testTrip()}}, nil
	}
	return *f.settings, nil
}

func (f *fakeSettings) Save(s domain.SettingsList) error {
	f.saves++
	if f.saveErr != nil {
		return f.saveErr
	}
	f.settings = &s
	return nil
}

func svc(no, typeID, typeName, dep, arr string) domain.Service {
	return domain.Service{
		TrainNo: no, TypeID: typeID, TypeName: typeName,
		SchedDep: at(dep), SchedArr: at(arr),
	}
}

func usualServices() []domain.Service {
	return []domain.Service{
		svc("1136", "1131", "區間", "08:16", "08:57"),
		svc("2008", "1132", "區間快", "08:26", "09:02"),
		svc("1138", "1131", "區間", "08:34", "09:14"),
	}
}

func testSettings() BriefSettings {
	return BriefSettings{
		Board:      2 * time.Minute,
		RiskMargin: 3 * time.Minute,
		Window:     domain.Window{Lookback: 30 * time.Minute, Lookahead: 60 * time.Minute},
		Filter: domain.TypeFilter{
			ExcludedIDs:   map[string]bool{"1101": true, "1107": true},
			KnownKeywords: []string{"區間快", "區間", "自強", "莒光"},
			Policy:        domain.IncludeAndFlag,
		},
		UsualTrainNos:       []string{"2008", "1136", "1138"},
		CertificateEnabled:  true,
		CertificateMinDelay: 5 * time.Minute,
		CompensationEnabled: true,
		SevereThreshold:     30 * time.Minute,
	}
}

// testTrip is the live settings (schedule, ready/deadline time, route, max
// early leave) that Brief.Run now takes per-call, in place of the fields that
// used to live on BriefSettings.
func testTrip() domain.Settings {
	return domain.Settings{
		Name:             "commute",
		ScheduleWeekdays: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
		ScheduleAt:       domain.TimeOfDay{Hour: 7, Minute: 50},
		ReadyAt:          domain.TimeOfDay{Hour: 8, Minute: 20},
		DeadlineAt:       domain.TimeOfDay{Hour: 9, Minute: 30},
		MaxEarlyLeave:    15 * time.Minute,
		OriginID:         "1080",
		OriginName:       "桃園",
		DestinationID:    "1000",
		DestinationName:  "臺北",
	}
}

// newTestBrief wires the use case against fakes, with sleeping disabled so
// delivery retries do not slow the suite down.
func newTestBrief(tt *fakeTimetable, d *fakeDelays, r *fakeRenderer, n *fakeNotifier) *Brief {
	return &Brief{
		Timetable:   tt,
		Delays:      d,
		Renderer:    r,
		Notifier:    n,
		Log:         quietLogger(),
		Settings:    testSettings(),
		SendRetries: 3,
		SendBackoff: time.Millisecond,
		Sleep:       func(time.Duration) {},
	}
}
