package domain

import "time"

// Certificate is the §7.6 answer to "which train do I quote at the counter?".
//
// TRA will issue a delay certificate for any delayed train that has already
// arrived, not only the one the user actually rode — they travel on a stored
// value card, so TRA cannot tell which train that was. That makes this a pure
// lookup over the trains sharing the user's window, with no bearing on which
// train to recommend.
type Certificate struct {
	// Found reports whether any train qualifies.
	Found bool
	// TrainNo is the qualifying train with the largest delay.
	TrainNo string
	// Delay is that train's delay — the minutes the certificate is worth.
	Delay time.Duration
	// Covered reports whether that is enough to account for the user's own
	// lateness.
	Covered bool
}

// DelayMinutes returns the certifiable minutes.
func (c Certificate) DelayMinutes() int { return int(c.Delay / time.Minute) }

// FindCertificate picks the most delayed train that will already have arrived
// by the time the user reaches the destination counter.
//
// The "already arrived" rule is encoded as EstArr <= the user's own arrival.
// In practice it rarely bites, since the worst-delayed train is usually the one
// ahead of you — but it costs one comparison, so there is no reason to leave it
// out.
//
// The candidate set is an approximation of "every delayed train that has
// arrived": it covers only the user's own window. Those are the trains on the
// same line at the same hour, so they represent the morning's disruption well,
// and using them needs no extra API call.
func FindCertificate(plan Plan, recommended Candidate, minDelay time.Duration) Certificate {
	atCounter := recommended.EstArr
	lateness := recommended.Lateness

	var best *Candidate
	for i := range plan.Candidates {
		c := &plan.Candidates[i]
		if c.DelaySource == DelaySourceNone {
			continue
		}
		if c.Delay < minDelay {
			continue
		}
		if c.EstArr.After(atCounter) {
			continue
		}
		if best == nil || c.Delay > best.Delay ||
			(c.Delay == best.Delay && c.TrainNo < best.TrainNo) {
			best = c
		}
	}
	if best == nil {
		return Certificate{}
	}
	return Certificate{
		Found:   true,
		TrainNo: best.TrainNo,
		Delay:   best.Delay,
		Covered: best.Delay >= lateness,
	}
}
