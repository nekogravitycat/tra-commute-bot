package domain

import (
	"testing"
	"time"
)

// The scenarios in §7.7 are all set on one morning, with the parameters the
// specification fixes: T_ready 08:20, boarding buffer 2, risk margin 3,
// deadline (destination-station arrival) 09:10 — equivalent to the old
// clock-in deadline of 09:30 minus a 20-minute last mile, which is where
// these fixtures originated. That makes CATCHABLE start at 08:25.
var testLoc = time.FixedZone("Asia/Taipei", 8*3600)

func at(hhmm string) time.Time {
	t, err := time.ParseInLocation("15:04", hhmm, testLoc)
	if err != nil {
		panic(err)
	}
	return time.Date(2026, 8, 18, t.Hour(), t.Minute(), 0, 0, testLoc)
}

func testParams() Params {
	return Params{
		Ready:       at("08:20"),
		Deadline:    at("09:10"),
		BoardBuffer: 2 * time.Minute,
		RiskMargin:  3 * time.Minute,
	}
}

// The three habitual trains, with the timetable confirmed against the live API.
func svc(no, typeID, typeName, dep, arr string) Service {
	return Service{
		TrainNo:  no,
		TypeID:   typeID,
		TypeCode: "6",
		TypeName: typeName,
		SchedDep: at(dep),
		SchedArr: at(arr),
	}
}

func usualServices() []Service {
	return []Service{
		svc("1136", "1131", "區間", "08:16", "08:57"),
		svc("2008", "1132", "區間快", "08:26", "09:02"),
		svc("1138", "1131", "區間", "08:34", "09:14"),
	}
}

func testFilter() TypeFilter {
	return TypeFilter{
		ExcludedIDs:      map[string]bool{"1101": true, "1107": true},
		ExcludedKeywords: []string{"普悠瑪", "太魯閣"},
		KnownKeywords:    []string{"區間快", "區間", "自強", "莒光", "復興", "普快"},
		Policy:           IncludeAndFlag,
	}
}

func buildTestPlan(t *testing.T, services []Service, delays map[string]int) Plan {
	t.Helper()
	return BuildPlan(PlanInput{
		Services:      services,
		Delays:        delays,
		Params:        testParams(),
		Window:        Window{Lookback: 30 * time.Minute, Lookahead: 60 * time.Minute},
		Filter:        testFilter(),
		UsualTrainNos: []string{"2008", "1136", "1138"},
	})
}

func find(t *testing.T, p Plan, no string) Candidate {
	t.Helper()
	for _, c := range p.Candidates {
		if c.TrainNo == no {
			return c
		}
	}
	t.Fatalf("train %s not among candidates", no)
	return Candidate{}
}

func trainNos(cs []Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.TrainNo
	}
	return out
}
