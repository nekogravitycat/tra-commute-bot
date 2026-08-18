package domain

import (
	"testing"
	"time"
)

const certMinDelay = 5 * time.Minute

// TestCertificateCovered is the ordinary late morning: a badly delayed train
// has already pulled in by the time the user reaches the counter, and it covers
// their own lateness in full.
//
// The certificate need not be for the train the user rode. They travel on a
// stored value card, so TRA cannot tell which one that was, and the counter
// will issue for any delayed train that has arrived.
func TestCertificateCovered(t *testing.T) {
	services := []Service{
		// Long gone by the time the user reaches the platform, but 24 minutes
		// late and already at the destination by 08:59.
		svc("3100", "1131", "區間", "07:50", "08:35"),
		// The recommendation: catchable, six late, arrives 09:11, which puts
		// the user one minute past the deadline.
		svc("3101", "1131", "區間", "08:30", "09:05"),
	}
	plan := buildTestPlan(t, services, map[string]int{"3100": 24, "3101": 6})
	rec := *plan.Recommended

	if rec.TrainNo != "3101" || rec.LatenessMinutes() != 1 {
		t.Fatalf("baseline = %s late %d min, want 3101 late 1 min",
			rec.TrainNo, rec.LatenessMinutes())
	}

	cert := FindCertificate(plan, rec, certMinDelay)
	if !cert.Found {
		t.Fatal("no certificate found despite a 24 minute delay on the line")
	}
	if cert.TrainNo != "3100" {
		t.Errorf("certificate train = %s, want 3100 (the worst delay that has arrived)",
			cert.TrainNo)
	}
	if cert.DelayMinutes() != 24 {
		t.Errorf("certifiable = %d min, want 24", cert.DelayMinutes())
	}
	if !cert.Covered {
		t.Errorf("24 certifiable minutes should cover %d minutes of lateness",
			rec.LatenessMinutes())
	}
}

// TestCertificateNotCovered is the case that needs careful wording: something
// can be certified, but not enough of it, and the message must not overpromise.
func TestCertificateNotCovered(t *testing.T) {
	// 1138 is the only catchable train and runs 30 late; 1136 is 6 late.
	services := usualServices()
	plan := buildTestPlan(t, services, map[string]int{"1136": 6, "2008": 60, "1138": 30})
	rec := *plan.Recommended

	if rec.TrainNo != "1138" {
		t.Fatalf("baseline = %s, want 1138", rec.TrainNo)
	}
	cert := FindCertificate(plan, rec, certMinDelay)
	if !cert.Found {
		t.Fatal("expected a certificate to be available")
	}
	if cert.Covered {
		t.Errorf("certificate of %d min must not claim to cover %d min of lateness",
			cert.DelayMinutes(), rec.LatenessMinutes())
	}
}

// TestCertificateNoneOnPunctualLine is the only case that genuinely warrants a
// warning: the user is late while the railway is running fine, so there is
// nothing to certify and the cause lies in their own timings.
func TestCertificateNoneOnPunctualLine(t *testing.T) {
	// Everything punctual, but the last mile is long enough to make even the
	// best train late.
	p := testParams()
	p.LastMile = 45 * time.Minute
	plan := BuildPlan(PlanInput{
		Services: usualServices(),
		Delays:   map[string]int{"1136": 0, "2008": 0, "1138": 0},
		Params:   p,
		Window:   Window{Lookback: 30 * time.Minute, Lookahead: 60 * time.Minute},
		Filter:   testFilter(),
	})
	rec := *plan.Recommended
	if rec.Lateness == 0 {
		t.Fatal("test setup should produce a late morning")
	}

	if cert := FindCertificate(plan, rec, certMinDelay); cert.Found {
		t.Errorf("certificate = %s, want none on a punctual line", cert.TrainNo)
	}
}

// TestCertificateExcludesNotYetArrived encodes the counter's one real rule: a
// certificate can only be issued for a train that has already arrived.
func TestCertificateExcludesNotYetArrived(t *testing.T) {
	services := []Service{
		// The recommendation: catchable, arrives 09:10.
		svc("3001", "1131", "區間", "08:30", "09:10"),
		// Far more delayed, but it pulls in after the user has left the
		// counter, so it cannot be quoted.
		svc("3002", "1131", "區間", "08:40", "09:15"),
	}
	plan := buildTestPlan(t, services, map[string]int{"3001": 6, "3002": 45})
	rec := *plan.Recommended
	if rec.TrainNo != "3001" {
		t.Fatalf("baseline = %s, want 3001", rec.TrainNo)
	}

	cert := FindCertificate(plan, rec, certMinDelay)
	if cert.TrainNo == "3002" {
		t.Error("quoted a train that has not arrived by the time the user is at the counter")
	}
	if !cert.Found || cert.TrainNo != "3001" {
		t.Errorf("certificate = %+v, want the user's own train 3001", cert)
	}
}

// TestCertificateBelowThreshold checks trivial delays are ignored: a trip to
// the counter is not worth making for two minutes.
func TestCertificateBelowThreshold(t *testing.T) {
	plan := buildTestPlan(t, usualServices(), map[string]int{"1136": 2, "2008": 3, "1138": 2})
	rec := *plan.Recommended

	if cert := FindCertificate(plan, rec, certMinDelay); cert.Found {
		t.Errorf("certificate = %s (%d min), want none below the %v threshold",
			cert.TrainNo, cert.DelayMinutes(), certMinDelay)
	}
}

// TestCertificateIgnoresUnobservedDelays checks a train with no live record is
// never quoted: its zero delay is an assumption, not an observation, and the
// counter would have nothing to issue.
func TestCertificateIgnoresUnobservedDelays(t *testing.T) {
	plan := buildTestPlan(t, usualServices(), map[string]int{})
	rec := *plan.Recommended

	if cert := FindCertificate(plan, rec, 0); cert.Found {
		t.Errorf("certificate = %s, want none when no delay was ever observed", cert.TrainNo)
	}
}

// TestCertificateDeterministicTie checks equally delayed trains resolve the
// same way every time, so two runs of the same morning quote the same number.
func TestCertificateDeterministicTie(t *testing.T) {
	services := []Service{
		svc("3010", "1131", "區間", "08:30", "09:05"),
		svc("3011", "1131", "區間", "08:31", "09:06"),
		svc("3012", "1131", "區間", "08:32", "09:07"),
	}
	plan := buildTestPlan(t, services, map[string]int{"3010": 10, "3011": 10, "3012": 10})
	rec := plan.Candidates[len(plan.Candidates)-1]

	for i := 0; i < 5; i++ {
		if got := FindCertificate(plan, rec, certMinDelay).TrainNo; got != "3010" {
			t.Fatalf("certificate = %s on run %d, want the lowest number 3010", got, i)
		}
	}
}
