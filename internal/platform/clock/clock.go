// Package clock supplies the current time, real or simulated.
package clock

import "time"

// Real reads the system clock in a fixed location. The location is explicit
// rather than local so the program behaves identically wherever it runs.
type Real struct{ Loc *time.Location }

// Now returns the current time in the configured location.
func (r Real) Now() time.Time { return time.Now().In(r.Loc) }

// Fixed always returns the same instant. It backs the -at flag, which is not a
// convenience: a program that runs once a day is otherwise impossible to debug,
// because every experiment costs a full day to observe.
type Fixed struct{ At time.Time }

// Now returns the fixed instant.
func (f Fixed) Now() time.Time { return f.At }
