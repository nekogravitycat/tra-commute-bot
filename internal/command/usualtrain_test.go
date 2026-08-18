package command

import (
	"strings"
	"testing"
)

func TestUsualTrainAddAndList(t *testing.T) {
	r, mb, store := newTestRouter(t)

	sendText(r, "/usualtrain")
	if !strings.Contains(mb.last(), "尚未設定") {
		t.Errorf("empty list message = %q, want it to say nothing is set yet", mb.last())
	}

	sendCallback(r, cbUsualTrainAdd)
	sendText(r, "2008")

	list, _ := store.Load()
	if len(list.UsualTrainNos) != 1 || list.UsualTrainNos[0] != "2008" {
		t.Fatalf("usual train nos = %v, want [2008]", list.UsualTrainNos)
	}
	if !strings.Contains(mb.last(), "2008") {
		t.Errorf("last message = %q, want it to list 2008", mb.last())
	}
}

func TestUsualTrainRejectsNonNumeric(t *testing.T) {
	r, mb, store := newTestRouter(t)

	sendText(r, "/usualtrain")
	sendCallback(r, cbUsualTrainAdd)
	sendText(r, "abcd")

	if !strings.Contains(mb.last(), "格式不對") {
		t.Errorf("last message = %q, want a format-rejection prompt", mb.last())
	}
	list, _ := store.Load()
	if len(list.UsualTrainNos) != 0 {
		t.Errorf("rejected input must not be persisted, got %v", list.UsualTrainNos)
	}

	// Recovering with a valid number must still work: the session should
	// still be waiting on the same prompt.
	sendText(r, "2008")
	list, _ = store.Load()
	if len(list.UsualTrainNos) != 1 || list.UsualTrainNos[0] != "2008" {
		t.Errorf("usual train nos = %v, want [2008] after a valid retry", list.UsualTrainNos)
	}
}

func TestUsualTrainAddDoesNotDuplicate(t *testing.T) {
	r, _, store := newTestRouter(t)

	sendText(r, "/usualtrain")
	sendCallback(r, cbUsualTrainAdd)
	sendText(r, "2008")
	sendCallback(r, cbUsualTrainAdd)
	sendText(r, "2008")

	list, _ := store.Load()
	if len(list.UsualTrainNos) != 1 {
		t.Errorf("usual train nos = %v, want a single 2008, not a duplicate", list.UsualTrainNos)
	}
}

func TestUsualTrainRemove(t *testing.T) {
	r, mb, store := newTestRouter(t)

	sendText(r, "/usualtrain")
	sendCallback(r, cbUsualTrainAdd)
	sendText(r, "2008")
	sendCallback(r, cbUsualTrainAdd)
	sendText(r, "1136")

	sendCallback(r, cbUsualTrainDel+"2008")

	list, _ := store.Load()
	if len(list.UsualTrainNos) != 1 || list.UsualTrainNos[0] != "1136" {
		t.Fatalf("usual train nos = %v, want [1136]", list.UsualTrainNos)
	}
	if strings.Contains(mb.last(), "2008") {
		t.Errorf("last message = %q, should no longer list the removed 2008", mb.last())
	}
}

// TestUsualTrainDoesNotDisturbSchedules checks /usualtrain edits the
// list-wide UsualTrainNos without touching any Schedule — a regression this
// guards against is SettingsList.Upsert dropping UsualTrainNos, or an
// unrelated write here dropping a Schedule.
func TestUsualTrainDoesNotDisturbSchedules(t *testing.T) {
	r, _, store := newTestRouter(t)
	runSetup(t, r, "上班通勤")

	sendText(r, "/usualtrain")
	sendCallback(r, cbUsualTrainAdd)
	sendText(r, "2008")

	list, _ := store.Load()
	if _, ok := list.Find("上班通勤"); !ok {
		t.Error("adding a usual train must not remove an existing schedule")
	}
	if len(list.UsualTrainNos) != 1 || list.UsualTrainNos[0] != "2008" {
		t.Errorf("usual train nos = %v, want [2008]", list.UsualTrainNos)
	}

	// And the reverse: creating a schedule afterwards must not wipe the
	// usual-train list back out (SettingsList.Upsert must preserve it).
	runSetup(t, r, "下班通勤")
	list, _ = store.Load()
	if len(list.UsualTrainNos) != 1 || list.UsualTrainNos[0] != "2008" {
		t.Errorf("usual train nos = %v after a later /setup, want unchanged [2008]", list.UsualTrainNos)
	}
}

func TestUsualTrainDoneClearsSession(t *testing.T) {
	r, mb, store := newTestRouter(t)

	sendText(r, "/usualtrain")
	sendCallback(r, cbUsualTrainDone)
	if !strings.Contains(mb.last(), "usualtrain") {
		t.Errorf("last message = %q, want it to point back at /usualtrain", mb.last())
	}

	// The session should be resting, not awaiting free-text input: a stray
	// message must not be swallowed as a train number.
	sendText(r, "9999")
	list, _ := store.Load()
	if len(list.UsualTrainNos) != 0 {
		t.Errorf("a message sent after 完成 must not be added, got %v", list.UsualTrainNos)
	}
}
