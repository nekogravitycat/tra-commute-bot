package domain

import "strings"

// UnknownTypePolicy decides what to do with a train whose type matches neither
// the exclusion list nor any recognised type name.
type UnknownTypePolicy int

const (
	// IncludeAndFlag keeps the train as a candidate but marks it so the
	// message can warn the reader to check before boarding.
	//
	// This is the default because the two possible errors cost very different
	// amounts: recommending an unboardable train is discovered on the
	// platform and corrected, whereas silently dropping a boardable one is
	// never discovered at all.
	IncludeAndFlag UnknownTypePolicy = iota
	// ExcludeUnknown drops the train from the candidate list.
	ExcludeUnknown
)

// ParseUnknownTypePolicy maps the config string onto the policy.
func ParseUnknownTypePolicy(s string) (UnknownTypePolicy, bool) {
	switch s {
	case "include_and_flag":
		return IncludeAndFlag, true
	case "exclude":
		return ExcludeUnknown, true
	default:
		return IncludeAndFlag, false
	}
}

// TypeFilter implements the A13 rule: keep trains that accept electronic
// tickets, drop the ones that do not.
//
// It is an exclusion list rather than an allow list. When TRA introduces a new
// train type, an allow list would silently drop a perfectly boardable train,
// while an exclusion list at worst lets through one the user can see for
// themselves is wrong.
type TypeFilter struct {
	// ExcludedIDs holds TrainTypeIDs to drop. IDs are the reliable key:
	// TrainTypeCode is shared across types (a plain 自強 and 普悠瑪 differ by
	// ID but 自強 alone spans codes 3 and 11).
	ExcludedIDs map[string]bool
	// ExcludedKeywords is a defensive fallback matched against the Chinese
	// type name, in case TRA reshuffles the IDs.
	ExcludedKeywords []string
	// KnownKeywords lists the type names known to accept electronic tickets.
	// A type matching none of them is Unknown rather than eligible.
	KnownKeywords []string
	// Policy decides the fate of an unknown type.
	Policy UnknownTypePolicy
}

// Eligibility classifies one train type.
func (f TypeFilter) Eligibility(typeID, typeName string) TicketEligibility {
	if f.ExcludedIDs[typeID] {
		return TicketIneligible
	}
	for _, kw := range f.ExcludedKeywords {
		if kw != "" && strings.Contains(typeName, kw) {
			return TicketIneligible
		}
	}
	for _, kw := range f.KnownKeywords {
		if kw != "" && strings.Contains(typeName, kw) {
			return TicketEligible
		}
	}
	return TicketUnknown
}
