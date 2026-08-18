package domain

import "strings"

// Station is one TRA station, as needed to resolve a name the user typed
// into a station ID during /setup or /manage's route question (§10.4-A).
type Station struct {
	ID     string
	NameZh string
	NameEn string
}

// MatchStations ranks stations against a query the user typed into /setup or
// /manage's station question. It is deliberately simple substring matching
// rather than anything
// distance-based: TRA station names are short and mostly distinct, and a
// user picking from an inline keyboard can tolerate a slightly noisy
// candidate list far better than a fuzzy matcher can tolerate silently
// picking the wrong station.
//
// Ranking, best first:
//  1. exact station ID
//  2. exact name match (either language)
//  3. name starts with the query
//  4. name contains the query
//
// Within a tier, order is stable (the input order), which for the TDX
// station list is roughly geographic and good enough to be predictable.
func MatchStations(stations []Station, query string) []Station {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	qLower := strings.ToLower(q)

	var exact, starts, contains []Station
	for _, s := range stations {
		if s.ID == q {
			return []Station{s}
		}
		zh, en := s.NameZh, strings.ToLower(s.NameEn)
		switch {
		case zh == q || en == qLower:
			exact = append(exact, s)
		case strings.HasPrefix(zh, q) || strings.HasPrefix(en, qLower):
			starts = append(starts, s)
		case strings.Contains(zh, q) || strings.Contains(en, qLower):
			contains = append(contains, s)
		}
	}

	out := make([]Station, 0, len(exact)+len(starts)+len(contains))
	out = append(out, exact...)
	out = append(out, starts...)
	out = append(out, contains...)
	return out
}
