package usecase

import (
	"context"
	"log/slog"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

// Board answers one /shortcuts trigger: today's remaining departures for one
// route, live-delay applied but not ranked. Unlike Brief it needs no ready
// time, deadline, certificate or compensation logic — a shortcut asks "what
// is running", not "what should I catch", so there is nothing here to
// classify or recommend.
type Board struct {
	Timetable TimetableSource
	Delays    DelaySource
	// Filter is the same A13 ticket-eligibility rule BuildPlan applies,
	// shared from config.yaml rather than set per Shortcut, since it is a
	// calibration knob rather than a fact about one route.
	Filter domain.TypeFilter
	Log    *slog.Logger
}

// BoardResult is everything the renderer needs to answer one shortcut query.
type BoardResult struct {
	Route       domain.Route
	GeneratedAt time.Time
	Board       domain.Board

	LiveDataAvailable bool
	DataUpdatedAt     time.Time
	// TimetableErr means the timetable fetch itself failed: there is
	// nothing left to fall back to, unlike a live-board failure.
	TimetableErr bool
}

// Query runs one shortcut. Like Brief.Run, a live-board failure still
// returns scheduled times rather than nothing — see CLAUDE.md's "nothing
// fails silently" — but a timetable failure has no fallback: without a
// timetable there are no rows to show at all.
func (b *Board) Query(ctx context.Context, originID, destID string, route domain.Route, now time.Time) BoardResult {
	res := BoardResult{Route: route, GeneratedAt: now}

	timetable, err := b.Timetable.DailyODTimetable(ctx, originID, destID, now)
	if err != nil {
		b.Log.Error("shortcut timetable fetch failed", "err", err)
		res.TimetableErr = true
		return res
	}

	delays := DelaySnapshot{ByTrainNo: map[string]int{}}
	live := true
	if snap, err := b.Delays.LiveDelays(ctx); err != nil {
		b.Log.Error("shortcut live board fetch failed", "err", err)
		live = false
	} else {
		delays = snap
	}

	res.Board = domain.BuildBoard(timetable.Services, delays.ByTrainNo, b.Filter, now)
	res.LiveDataAvailable = live
	res.DataUpdatedAt = delays.UpdatedAt
	return res
}
