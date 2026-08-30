package command

import (
	"strings"
	"testing"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
	"github.com/nekogravitycat/tra-commute-bot/internal/usecase"
)

// runShortcutSetup drives /shortcuts' add flow to completion: a trigger
// word, a unique-match origin, and an ambiguous-match destination resolved
// by picking the first candidate — the same station subflow /setup's
// runSetup exercises, reused here.
func runShortcutSetup(t *testing.T, r *Router, trigger string) {
	t.Helper()
	sendText(r, "/shortcuts")
	sendCallback(r, cbShortcutAdd)
	sendText(r, trigger)
	sendText(r, "臺北")              // origin: unique match
	sendText(r, "新")               // destination: ambiguous -> keyboard
	sendCallback(r, cbStation+"0") // pick first candidate (新竹)
}

func TestShortcutAddAndList(t *testing.T) {
	r, mb, store := newTestRouter(t)

	sendText(r, "/shortcuts")
	if !strings.Contains(mb.last(), "尚未設定") {
		t.Errorf("empty list message = %q, want it to say nothing is set yet", mb.last())
	}

	runShortcutSetup(t, r, "home")

	list, _ := store.Load()
	if len(list.Shortcuts) != 1 {
		t.Fatalf("shortcuts = %+v, want 1", list.Shortcuts)
	}
	sc := list.Shortcuts[0]
	if sc.Trigger != "home" || sc.OriginName != "臺北" || sc.DestinationName != "新竹" {
		t.Errorf("shortcut = %+v, want home: 臺北 -> 新竹", sc)
	}
	if !strings.Contains(mb.last(), "home") {
		t.Errorf("last message = %q, want it to list home", mb.last())
	}
}

func TestShortcutAddRejectsDuplicateTriggerCaseInsensitive(t *testing.T) {
	r, mb, store := newTestRouter(t)
	runShortcutSetup(t, r, "home")

	sendText(r, "/shortcuts")
	sendCallback(r, cbShortcutAdd)
	sendText(r, "HOME") // same trigger, different case
	if !strings.Contains(mb.last(), "已經是另一個捷徑") {
		t.Errorf("last message = %q, want a duplicate-trigger rejection", mb.last())
	}

	list, _ := store.Load()
	if len(list.Shortcuts) != 1 {
		t.Errorf("shortcuts = %+v, want still just 1 (the rejected duplicate must not be added)", list.Shortcuts)
	}
}

func TestShortcutAddRejectsReservedTrigger(t *testing.T) {
	r, mb, store := newTestRouter(t)

	sendText(r, "/shortcuts")
	sendCallback(r, cbShortcutAdd)
	sendText(r, "setup")
	if !strings.Contains(mb.last(), "保留的指令名稱") {
		t.Errorf("last message = %q, want a reserved-word rejection", mb.last())
	}

	list, _ := store.Load()
	if len(list.Shortcuts) != 0 {
		t.Errorf("shortcuts = %+v, want none (a reserved trigger must not be added)", list.Shortcuts)
	}
}

func TestShortcutRemove(t *testing.T) {
	r, mb, store := newTestRouter(t)
	runShortcutSetup(t, r, "home")

	sendCallback(r, cbShortcutDel+"home")

	list, _ := store.Load()
	if len(list.Shortcuts) != 0 {
		t.Errorf("shortcuts = %+v, want none after removal", list.Shortcuts)
	}
	if strings.Contains(mb.last(), "home") {
		t.Errorf("last message = %q, should no longer list the removed home", mb.last())
	}
}

func TestShortcutTriggerRunsBoardQuery(t *testing.T) {
	r, mb, _ := newTestRouter(t)
	runShortcutSetup(t, r, "home")

	r.Board.Timetable = &fakeTimetable{timetable: usecase.Timetable{
		Services: []domain.Service{
			{TrainNo: "2008", TypeID: "1132", TypeName: "區間快", SchedDep: time.Now().Add(10 * time.Minute), SchedArr: time.Now().Add(40 * time.Minute)},
		},
	}}
	r.Board.Delays = &fakeDelays{snapshot: usecase.DelaySnapshot{ByTrainNo: map[string]int{"2008": 3}}}

	sendText(r, "home")

	if !strings.Contains(mb.last(), "臺北 → 新竹") {
		t.Errorf("last message = %q, want the home shortcut's route", mb.last())
	}
	if !strings.Contains(mb.last(), "2008") {
		t.Errorf("last message = %q, want the queried train listed", mb.last())
	}
}

func TestShortcutTriggerCaseInsensitiveMatch(t *testing.T) {
	r, mb, _ := newTestRouter(t)
	runShortcutSetup(t, r, "home")

	sendText(r, "HOME")
	if !strings.Contains(mb.last(), "臺北 → 新竹") {
		t.Errorf("last message = %q, want a case-insensitive trigger match", mb.last())
	}
}

// TestActiveFlowTakesPriorityOverShortcutTrigger checks that mid-flow text
// answering a question (e.g. /setup's name prompt) is never hijacked by a
// coincidentally matching shortcut trigger.
func TestActiveFlowTakesPriorityOverShortcutTrigger(t *testing.T) {
	r, mb, _ := newTestRouter(t)
	runShortcutSetup(t, r, "home")

	sendText(r, "/setup")
	sendText(r, "home") // matches the shortcut trigger, but should be taken as the schedule's name

	// Taking "home" as the name must have advanced the flow to the next
	// question (起始站) rather than running the shortcut's board query.
	if !strings.Contains(mb.last(), "起始站") {
		t.Errorf("last message = %q, want the flow to have advanced to the origin question", mb.last())
	}
	if strings.Contains(mb.last(), "臺北 → 新竹") {
		t.Error("a shortcut trigger typed mid-flow must not run the board query")
	}
}

func TestUnrecognisedTextIsNotTreatedAsShortcut(t *testing.T) {
	r, mb, _ := newTestRouter(t)
	sendText(r, "not a real trigger")
	if !strings.Contains(mb.last(), "/setup") {
		t.Errorf("last message = %q, want the usual fallback for unrecognised text", mb.last())
	}
}
