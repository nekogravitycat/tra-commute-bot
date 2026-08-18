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
	qZh := normalizeZh(q)
	qLower := strings.ToLower(q)

	var exact, starts, contains []Station
	for _, s := range stations {
		if s.ID == q {
			return []Station{s}
		}
		zh, en := normalizeZh(s.NameZh), strings.ToLower(s.NameEn)
		switch {
		case zh == qZh || en == qLower:
			exact = append(exact, s)
		case strings.HasPrefix(zh, qZh) || strings.HasPrefix(en, qLower):
			starts = append(starts, s)
		case strings.Contains(zh, qZh) || strings.Contains(en, qLower):
			contains = append(contains, s)
		}
	}

	out := make([]Station, 0, len(exact)+len(starts)+len(contains))
	out = append(out, exact...)
	out = append(out, starts...)
	out = append(out, contains...)
	return out
}

// zhVariants normalizes interchangeable Traditional Chinese character variants
// so a query typed with the common form still matches TRA's official name.
// TDX's /Station catalog spells every "Tai" station with 臺 (e.g. 臺北,
// 臺中, 臺南, 臺東), but 台 is the form most people actually type — they're
// the same word, and users shouldn't have to know which glyph TRA prefers.
var zhVariants = strings.NewReplacer("台", "臺")

func normalizeZh(s string) string {
	return zhVariants.Replace(s)
}
