package domain

import (
	"testing"
	"time"
)

func boardRowNos(rows []BoardRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.TrainNo
	}
	return out
}

func TestBuildBoardDropsDepartedTrains(t *testing.T) {
	b := BuildBoard(usualServices(), map[string]int{"1136": 0, "2008": 0, "1138": 0}, testFilter(), at("08:20"))

	// 1136 departs 08:16, before "now" and with no delay to save it.
	got := boardRowNos(b.Rows)
	for _, no := range got {
		if no == "1136" {
			t.Errorf("rows = %v, a departed train with no delay should have been dropped", got)
		}
	}
	if len(b.Rows) != 2 {
		t.Fatalf("rows = %v, want the 2 trains not yet departed", got)
	}
}

// TestBuildBoardKeepsDelayedDeparture checks the same "still at the
// platform" reasoning BuildPlan's lookback exists for: a train whose
// scheduled departure has passed may still be catchable if it is running
// late.
func TestBuildBoardKeepsDelayedDeparture(t *testing.T) {
	// 1136's scheduled departure (08:16) is before now (08:20), but a 10
	// minute delay pushes its estimated departure to 08:26.
	b := BuildBoard(usualServices(), map[string]int{"1136": 10}, testFilter(), at("08:20"))

	found := false
	for _, r := range b.Rows {
		if r.TrainNo == "1136" {
			found = true
			if r.DelayMinutes() != 10 {
				t.Errorf("delay = %d, want 10", r.DelayMinutes())
			}
		}
	}
	if !found {
		t.Errorf("rows = %v, a delayed-but-not-yet-departed train should still show", boardRowNos(b.Rows))
	}
}

func TestBuildBoardSortsByEstimatedDeparture(t *testing.T) {
	// 2008 (08:26) would sort before 1138 (08:34) on schedule, but a big
	// delay on 2008 should push it later in the board.
	b := BuildBoard(usualServices(), map[string]int{"2008": 30, "1138": 0}, testFilter(), at("08:00"))

	got := boardRowNos(b.Rows)
	idx := func(no string) int {
		for i, n := range got {
			if n == no {
				return i
			}
		}
		t.Fatalf("train %s not among rows %v", no, got)
		return -1
	}
	if idx("1138") > idx("2008") {
		t.Errorf("order = %v, want 1138 before the delayed 2008", got)
	}
}

func TestBuildBoardExcludesIneligibleType(t *testing.T) {
	services := append(usualServices(), svc("777", "1101", "普悠瑪", "08:40", "09:10"))
	b := BuildBoard(services, map[string]int{}, testFilter(), at("08:00"))

	for _, r := range b.Rows {
		if r.TrainNo == "777" {
			t.Errorf("rows = %v, ineligible type should have been excluded", boardRowNos(b.Rows))
		}
	}
	if b.ExcludedCount != 1 {
		t.Errorf("excluded count = %d, want 1", b.ExcludedCount)
	}
}

func TestBuildBoardFlagsUnknownType(t *testing.T) {
	services := append(usualServices(), svc("777", "9999", "磁浮特快", "08:40", "09:10"))
	b := BuildBoard(services, map[string]int{}, testFilter(), at("08:00"))

	if len(b.UnknownTypes) != 1 || b.UnknownTypes[0] != "磁浮特快" {
		t.Errorf("unknown types = %v, want [磁浮特快]", b.UnknownTypes)
	}
	found := false
	for _, r := range b.Rows {
		if r.TrainNo == "777" {
			found = true
		}
	}
	if !found {
		t.Error("IncludeAndFlag policy should keep the unknown-type train as a row")
	}
}

func TestBuildBoardExcludesSuspended(t *testing.T) {
	services := usualServices()
	services[0].Suspended = true
	b := BuildBoard(services, map[string]int{}, testFilter(), at("08:00"))

	if b.SuspendedCount != 1 {
		t.Errorf("suspended count = %d, want 1", b.SuspendedCount)
	}
	for _, r := range b.Rows {
		if r.TrainNo == services[0].TrainNo {
			t.Error("a suspended train must not appear as a row")
		}
	}
}

func TestBuildBoardEmpty(t *testing.T) {
	b := BuildBoard(nil, map[string]int{}, testFilter(), time.Now())
	if len(b.Rows) != 0 {
		t.Errorf("rows = %v, want none", b.Rows)
	}
}
