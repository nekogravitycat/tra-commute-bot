package domain

import "testing"

var testStations = []Station{
	{ID: "1080", NameZh: "桃園", NameEn: "Taoyuan"},
	{ID: "1000", NameZh: "臺北", NameEn: "Taipei"},
	{ID: "1010", NameZh: "板橋", NameEn: "Banciao"},
	{ID: "3300", NameZh: "新竹", NameEn: "Hsinchu"},
	{ID: "3210", NameZh: "新竹縣竹北", NameEn: "Zhubei"},
}

func TestMatchStationsExactID(t *testing.T) {
	got := MatchStations(testStations, "1080")
	if len(got) != 1 || got[0].ID != "1080" {
		t.Fatalf("got %+v, want exactly 1080", got)
	}
}

func TestMatchStationsExactName(t *testing.T) {
	got := MatchStations(testStations, "臺北")
	if len(got) == 0 || got[0].ID != "1000" {
		t.Fatalf("got %+v, want 臺北 ranked first", got)
	}
}

func TestMatchStationsTaiVariant(t *testing.T) {
	got := MatchStations(testStations, "台北")
	if len(got) == 0 || got[0].ID != "1000" {
		t.Fatalf("got %+v, want 臺北 ranked first for 台北 query", got)
	}
}

func TestMatchStationsEnglishCaseInsensitive(t *testing.T) {
	got := MatchStations(testStations, "taipei")
	if len(got) == 0 || got[0].ID != "1000" {
		t.Fatalf("got %+v, want 臺北 ranked first for lowercase English query", got)
	}
}

func TestMatchStationsPrefixBeatsContains(t *testing.T) {
	// "新竹" is an exact match for 新竹 (3300) and a prefix of 新竹縣竹北 (3210).
	got := MatchStations(testStations, "新竹")
	if len(got) < 2 || got[0].ID != "3300" || got[1].ID != "3210" {
		t.Fatalf("got %+v, want exact match 3300 ranked before prefix match 3210", got)
	}
}

func TestMatchStationsNoMatch(t *testing.T) {
	if got := MatchStations(testStations, "高雄"); len(got) != 0 {
		t.Errorf("got %+v, want no matches", got)
	}
}

func TestMatchStationsEmptyQuery(t *testing.T) {
	if got := MatchStations(testStations, "  "); got != nil {
		t.Errorf("got %+v, want nil for a blank query", got)
	}
}
