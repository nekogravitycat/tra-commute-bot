package tdx

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// newTestClient wires a client against a fake server with sleeping disabled, so
// the backoff schedule can be asserted without the tests taking half a minute.
func newTestClient(t *testing.T, handler http.Handler, slept *[]time.Duration) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := New(Credentials{ClientID: "id", ClientSecret: "secret"}, testLoc, Options{
		BaseURL: srv.URL,
		AuthURL: srv.URL + "/token",
		Log:     quietLogger(),
		Sleep: func(d time.Duration) {
			if slept != nil {
				*slept = append(*slept, d)
			}
		},
	})
	return c, srv
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestAuthenticate(t *testing.T) {
	var gotForm string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotForm = string(body)
		w.Write([]byte(`{"access_token":"tok-123","expires_in":86400,"token_type":"bearer"}`))
	}), nil)

	if err := c.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if c.token != "tok-123" {
		t.Errorf("token = %q, want tok-123", c.token)
	}
	for _, want := range []string{"grant_type=client_credentials", "client_id=id", "client_secret=secret"} {
		if !strings.Contains(gotForm, want) {
			t.Errorf("form %q missing %q", gotForm, want)
		}
	}
}

func TestAuthenticateRejectsEmptyToken(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"expires_in":86400}`))
	}), nil)

	if err := c.Authenticate(context.Background()); err == nil {
		t.Error("a response with no access_token must be an error, not a silent success")
	}
}

// TestUnauthenticatedRequest checks a missing token is caught locally rather
// than spent on a request that is certain to be rejected.
func TestUnauthenticatedRequest(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should reach the server without a token")
	}), nil)

	if _, err := c.LiveDelays(context.Background()); err == nil {
		t.Error("expected an error when not authenticated")
	}
}

func TestDailyODTimetable(t *testing.T) {
	var gotPath, gotAuth string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Query().Get("$format") != "JSON" {
			t.Errorf("$format = %q, want JSON", r.URL.Query().Get("$format"))
		}
		w.Write(fixture(t, "od_timetable.json"))
	}), nil)
	c.token = "tok"

	date := time.Date(2026, 8, 18, 7, 50, 0, 0, testLoc)
	tt, err := c.DailyODTimetable(context.Background(), "1080", "1000", date)
	if err != nil {
		t.Fatalf("DailyODTimetable: %v", err)
	}

	if want := "/DailyTrainTimetable/OD/1080/to/1000/2026-08-18"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("authorization = %q, want a bearer token", gotAuth)
	}
	if len(tt.Services) != 5 {
		t.Fatalf("services = %d, want 5", len(tt.Services))
	}

	byNo := map[string]int{}
	for i, s := range tt.Services {
		byNo[s.TrainNo] = i
	}
	s := tt.Services[byNo["2008"]]
	if got := s.SchedDep.Format("15:04"); got != "08:26" {
		t.Errorf("2008 departure = %s, want 08:26", got)
	}
	if got := s.SchedArr.Format("15:04"); got != "09:02" {
		t.Errorf("2008 arrival = %s, want 09:02", got)
	}
	if s.TypeID != "1132" || s.TypeName != "區間快" {
		t.Errorf("2008 type = %s/%s, want 1132/區間快", s.TypeID, s.TypeName)
	}
	if s.Suspended {
		t.Error("2008 should not be marked suspended")
	}
	// The type ID is what distinguishes the trains that refuse electronic
	// tickets; the code cannot, because 自強 spans two of them.
	if got := tt.Services[byNo["278"]].TypeID; got != "110G" {
		t.Errorf("278 type ID = %s, want 110G", got)
	}
	if !tt.ServiceDate.Equal(time.Date(2026, 8, 18, 0, 0, 0, 0, testLoc)) {
		t.Errorf("service date = %s, want 2026-08-18", tt.ServiceDate)
	}
}

// TestTimetableOvernight covers the defensive rollover. The line really does
// run a 23:28 → 00:02 service, and without this the arrival would appear to
// precede the departure by most of a day.
func TestTimetableOvernight(t *testing.T) {
	body := `{"TrainDate":"2026-08-18","UpdateTime":"2026-08-18T07:49:53+08:00","TrainTimetables":[
	  {"TrainInfo":{"TrainNo":"4999","TrainTypeID":"1131","TrainTypeCode":"6",
	    "TrainTypeName":{"Zh_tw":"區間","En":"Local Train"},"SuspendedFlag":0},
	   "StopTimes":[
	     {"StopSequence":8,"StationID":"1080","StationName":{"Zh_tw":"桃園"},"ArrivalTime":"23:26","DepartureTime":"23:28","SuspendedFlag":0},
	     {"StopSequence":17,"StationID":"1000","StationName":{"Zh_tw":"臺北"},"ArrivalTime":"00:02","DepartureTime":"00:04","SuspendedFlag":0}]}]}`

	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}), nil)
	c.token = "tok"

	tt, err := c.DailyODTimetable(context.Background(), "1080", "1000",
		time.Date(2026, 8, 18, 7, 50, 0, 0, testLoc))
	if err != nil {
		t.Fatalf("DailyODTimetable: %v", err)
	}
	if len(tt.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(tt.Services))
	}

	s := tt.Services[0]
	if !s.SchedArr.After(s.SchedDep) {
		t.Errorf("arrival %s does not follow departure %s", s.SchedArr, s.SchedDep)
	}
	if got := s.SchedArr.Day(); got != 19 {
		t.Errorf("arrival day = %d, want 19 (the next day)", got)
	}
	if got := s.SchedArr.Sub(s.SchedDep); got != 34*time.Minute {
		t.Errorf("journey = %v, want 34m", got)
	}
}

func TestTimetableSuspendedFlags(t *testing.T) {
	tests := []struct {
		name              string
		trainFlag, opFlag int
		want              bool
	}{
		{"running", 0, 0, false},
		{"whole service cancelled", 1, 0, true},
		{"skips this station", 0, 1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"TrainDate":"2026-08-18","TrainTimetables":[
			  {"TrainInfo":{"TrainNo":"1136","TrainTypeID":"1131",
			    "TrainTypeName":{"Zh_tw":"區間"},"SuspendedFlag":` + itoa(tc.trainFlag) + `},
			   "StopTimes":[
			     {"StopSequence":8,"StationID":"1080","ArrivalTime":"08:14","DepartureTime":"08:16","SuspendedFlag":` + itoa(tc.opFlag) + `},
			     {"StopSequence":17,"StationID":"1000","ArrivalTime":"08:57","DepartureTime":"08:59","SuspendedFlag":0}]}]}`

			c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(body))
			}), nil)
			c.token = "tok"

			tt, err := c.DailyODTimetable(context.Background(), "1080", "1000",
				time.Date(2026, 8, 18, 7, 50, 0, 0, testLoc))
			if err != nil {
				t.Fatalf("DailyODTimetable: %v", err)
			}
			if got := tt.Services[0].Suspended; got != tc.want {
				t.Errorf("suspended = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestTimetableSkipsUnusableEntries checks a malformed or irrelevant entry is
// dropped rather than taking the whole morning down with it.
func TestTimetableSkipsUnusableEntries(t *testing.T) {
	body := `{"TrainDate":"2026-08-18","TrainTimetables":[
	  {"TrainInfo":{"TrainNo":"BAD1","TrainTypeName":{"Zh_tw":"區間"}},
	   "StopTimes":[{"StopSequence":8,"StationID":"1080","DepartureTime":"nonsense"},
	                {"StopSequence":17,"StationID":"1000","ArrivalTime":"08:57"}]},
	  {"TrainInfo":{"TrainNo":"BAD2","TrainTypeName":{"Zh_tw":"區間"}},
	   "StopTimes":[{"StopSequence":8,"StationID":"1080","DepartureTime":"08:16"}]},
	  {"TrainInfo":{"TrainNo":"BAD3","TrainTypeName":{"Zh_tw":"區間"}},
	   "StopTimes":[{"StopSequence":30,"StationID":"1080","DepartureTime":"08:16"},
	                {"StopSequence":17,"StationID":"1000","ArrivalTime":"08:57"}]},
	  {"TrainInfo":{"TrainNo":"GOOD","TrainTypeName":{"Zh_tw":"區間"}},
	   "StopTimes":[{"StopSequence":8,"StationID":"1080","DepartureTime":"08:16"},
	                {"StopSequence":17,"StationID":"1000","ArrivalTime":"08:57"}]}]}`

	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}), nil)
	c.token = "tok"

	tt, err := c.DailyODTimetable(context.Background(), "1080", "1000",
		time.Date(2026, 8, 18, 7, 50, 0, 0, testLoc))
	if err != nil {
		t.Fatalf("DailyODTimetable: %v", err)
	}
	if len(tt.Services) != 1 || tt.Services[0].TrainNo != "GOOD" {
		t.Errorf("services = %v, want only GOOD", tt.Services)
	}
}

func TestLiveDelays(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/TrainLiveBoard" {
			t.Errorf("path = %q, want /TrainLiveBoard", r.URL.Path)
		}
		w.Write(fixture(t, "live_board.json"))
	}), nil)
	c.token = "tok"

	snap, err := c.LiveDelays(context.Background())
	if err != nil {
		t.Fatalf("LiveDelays: %v", err)
	}

	want := map[string]int{"1136": 8, "2008": 24, "1138": 2, "278": -3}
	for no, w := range want {
		if got, ok := snap.ByTrainNo[no]; !ok || got != w {
			t.Errorf("delay[%s] = %d (present %v), want %d", no, got, ok, w)
		}
	}
	// Negative values are passed through unchanged: clamping is a business
	// rule and belongs to the domain, not to the transport layer.
	if snap.ByTrainNo["278"] != -3 {
		t.Error("the adapter must not clamp; that is the domain's decision")
	}
	if got := snap.UpdatedAt.Format("15:04:05"); got != "07:49:53" {
		t.Errorf("updated at = %s, want 07:49:53", got)
	}
}

// TestRateLimitBackoff covers the response to a 429. TDX starts rejecting after
// roughly eight rapid requests; the real flow makes three, so this is a
// safety net rather than an expected path.
func TestRateLimitBackoff(t *testing.T) {
	var calls atomic.Int32
	var slept []time.Duration

	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"message":"API rate limit exceeded"}`))
			return
		}
		w.Write(fixture(t, "live_board.json"))
	}), &slept)
	c.token = "tok"

	if _, err := c.LiveDelays(context.Background()); err != nil {
		t.Fatalf("LiveDelays should have recovered: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("requests = %d, want 3 (two rejections then success)", got)
	}
	want := []time.Duration{5 * time.Second, 10 * time.Second}
	if len(slept) != len(want) {
		t.Fatalf("waits = %v, want %v", slept, want)
	}
	for i := range want {
		if slept[i] != want[i] {
			t.Errorf("wait %d = %v, want %v", i, slept[i], want[i])
		}
	}
}

// TestRateLimitExhausted checks the client gives up after the schedule is
// spent, which is what triggers the degraded notification upstream.
func TestRateLimitExhausted(t *testing.T) {
	var calls atomic.Int32
	var slept []time.Duration

	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}), &slept)
	c.token = "tok"

	_, err := c.LiveDelays(context.Background())
	if err == nil {
		t.Fatal("expected an error once the backoff schedule is exhausted")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error %q should name the rate limit", err)
	}
	if got := calls.Load(); got != 4 {
		t.Errorf("requests = %d, want 4 (the initial try plus three retries)", got)
	}
	want := []time.Duration{5 * time.Second, 10 * time.Second, 15 * time.Second}
	if len(slept) != len(want) {
		t.Errorf("waits = %v, want %v", slept, want)
	}
}

// TestServerErrorNotRetried checks a 500 fails immediately. Only rate limiting
// is worth waiting out; retrying a broken endpoint just delays the fallback.
func TestServerErrorNotRetried(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("upstream exploded"))
	}), nil)
	c.token = "tok"

	if _, err := c.LiveDelays(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
}

// TestThrottleSpacesRequests checks the configured gap is honoured, which is
// what keeps the flow clear of the rate limit in the first place.
func TestThrottleSpacesRequests(t *testing.T) {
	var slept []time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(fixture(t, "live_board.json"))
	}))
	defer srv.Close()

	c := New(Credentials{}, testLoc, Options{
		BaseURL:  srv.URL,
		Interval: 1500 * time.Millisecond,
		Log:      quietLogger(),
		Sleep:    func(d time.Duration) { slept = append(slept, d) },
	})
	c.token = "tok"

	ctx := context.Background()
	if _, err := c.LiveDelays(ctx); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(slept) != 0 {
		t.Errorf("the first request should not wait, slept %v", slept)
	}
	if _, err := c.LiveDelays(ctx); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(slept) != 1 || slept[0] <= 0 || slept[0] > 1500*time.Millisecond {
		t.Errorf("waits = %v, want one wait of at most 1.5s", slept)
	}
}

// TestArchiverReceivesRawBody checks the raw response is captured. When a
// recommendation turns out wrong, this copy is the only remaining evidence:
// TDX has moved on within minutes.
func TestArchiverReceivesRawBody(t *testing.T) {
	got := map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(fixture(t, "live_board.json"))
	}))
	defer srv.Close()

	c := New(Credentials{}, testLoc, Options{
		BaseURL:  srv.URL,
		Log:      quietLogger(),
		Sleep:    func(time.Duration) {},
		Archiver: func(name string, payload []byte) { got[name] = payload },
	})
	c.token = "tok"

	if _, err := c.LiveDelays(context.Background()); err != nil {
		t.Fatalf("LiveDelays: %v", err)
	}
	if body, ok := got["liveboard"]; !ok || !strings.Contains(string(body), "TrainLiveBoards") {
		t.Errorf("archived %d entries, want the raw live board body", len(got))
	}
}

func TestMalformedJSON(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"TrainLiveBoards": [ truncated`))
	}), nil)
	c.token = "tok"

	if _, err := c.LiveDelays(context.Background()); err == nil {
		t.Error("expected a decode error")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return "1"
}

// TestTokenNeverArchived guards a credential leak: the archive is a long-lived
// file on disk, and the OAuth response carries a live bearer token that has no
// diagnostic value in it.
func TestTokenNeverArchived(t *testing.T) {
	var archived []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			w.Write([]byte(`{"access_token":"super-secret-jwt","expires_in":86400}`))
			return
		}
		w.Write(fixture(t, "live_board.json"))
	}))
	defer srv.Close()

	c := New(Credentials{ClientID: "id", ClientSecret: "secret"}, testLoc, Options{
		BaseURL: srv.URL,
		AuthURL: srv.URL + "/token",
		Log:     quietLogger(),
		Sleep:   func(time.Duration) {},
		Archiver: func(name string, payload []byte) {
			archived = append(archived, name)
			if strings.Contains(string(payload), "super-secret-jwt") {
				t.Errorf("the access token was written to the archive under %q", name)
			}
		},
	})

	ctx := context.Background()
	if err := c.Authenticate(ctx); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if _, err := c.LiveDelays(ctx); err != nil {
		t.Fatalf("LiveDelays: %v", err)
	}

	for _, name := range archived {
		if name == "auth" {
			t.Error("the auth response must not be archived at all")
		}
	}
	if len(archived) != 1 || archived[0] != "liveboard" {
		t.Errorf("archived %v, want only the live board", archived)
	}
}
