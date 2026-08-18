// Package domain holds the business rules of the commute brief: the train
// model, the two-dimensional catchability/lateness classification (spec §7.4),
// the lexicographic ranking (§7.5), the delay-certificate search (§7.6), the
// early-departure compensation search (§7.8) and the schedule tick decision
// (§10.3).
//
// It is the innermost layer: it imports nothing outside the standard library
// and knows nothing about TDX, Telegram, YAML or the filesystem. Every input it
// needs arrives as a plain value; every decision it makes is a pure function of
// those values, which is what makes the scenario tests in §11 possible.
package domain
