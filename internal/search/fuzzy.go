// Package search ports the website's client-side fuzzy ranking to Go.
// Ranking parity with positronick.com is a product guarantee: FuzzyScore,
// ScoreSoul and ScoreListing are 1:1 ports of fuzzyScore and the relevance()
// field weights in src/lib/filter.ts and src/lib/listingFilter.ts of the
// product repo. Change those files first; mirror them here.
package search

import (
	"sort"
	"strings"

	"github.com/positronick/cli/internal/api"
)

// FuzzyScore is the fuzzy match score of needle against haystack: higher is
// better, 0 means no match. A contiguous substring beats a scattered
// subsequence; earlier matches beat later ones. Exact port of fuzzyScore in
// src/lib/filter.ts:
//
//   - empty (trimmed) needle: 1 — a universal match
//   - contiguous substring at index i: 2000 - i
//   - otherwise subsequence scoring: +5 for a char adjacent to the previous
//     match, +1 for any other matched char; 0 if any char is missing
func FuzzyScore(needle, haystack string) int {
	n := []rune(strings.ToLower(strings.TrimSpace(needle)))
	h := []rune(strings.ToLower(haystack))
	if len(n) == 0 {
		return 1
	}

	if idx := runeIndex(h, n); idx != -1 {
		return 2000 - idx // contiguous substring — earlier position ranks higher
	}

	// Fall back to subsequence matching, rewarding adjacent characters.
	hi := 0
	score := 0
	prevMatch := -2
	for _, c := range n {
		for hi < len(h) && h[hi] != c {
			hi++
		}
		if hi >= len(h) {
			return 0 // a character is missing — not a subsequence
		}
		if hi == prevMatch+1 {
			score += 5
		} else {
			score++
		}
		prevMatch = hi
		hi++
	}
	return score
}

// runeIndex returns the rune index of the first occurrence of sub in s, or
// -1. Rune-based (not byte-based) so the "2000 - index" contiguous score
// counts characters like the website's JavaScript String.indexOf does.
func runeIndex(s, sub []rune) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		j := 0
		for ; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				break
			}
		}
		if j == len(sub) {
			return i
		}
	}
	return -1
}

// ScoreSoul is the best fuzzy score of query across a soul's searchable
// fields, weighted by importance. Exact port of relevance() in
// src/lib/filter.ts: name x3, tagline x2, authorHandle x1, each tag and
// framework unweighted; an empty query scores 1 (matches everything).
func ScoreSoul(query string, s api.SoulCard) int {
	if strings.TrimSpace(query) == "" {
		return 1
	}
	best := FuzzyScore(query, s.Name) * 3
	best = max(best, FuzzyScore(query, s.Tagline)*2)
	best = max(best, FuzzyScore(query, s.AuthorHandle))
	for _, t := range s.Tags {
		best = max(best, FuzzyScore(query, t))
	}
	for _, f := range s.Frameworks {
		best = max(best, FuzzyScore(query, f))
	}
	return best
}

// ScoreListing is the best fuzzy score of query across a listing's searchable
// fields, weighted by importance. Exact port of relevance() in
// src/lib/listingFilter.ts: name x3, tagline x2, profileHandle x2, then
// profileName, type, description and each tag unweighted; an empty query
// scores 1.
func ScoreListing(query string, l api.Listing) int {
	if strings.TrimSpace(query) == "" {
		return 1
	}
	description := ""
	if l.Description != nil {
		description = *l.Description
	}
	best := FuzzyScore(query, l.Name) * 3
	best = max(best, FuzzyScore(query, l.Tagline)*2)
	best = max(best, FuzzyScore(query, l.ProfileHandle)*2)
	best = max(best, FuzzyScore(query, l.ProfileName))
	best = max(best, FuzzyScore(query, l.Type))
	best = max(best, FuzzyScore(query, description))
	for _, t := range l.Tags {
		best = max(best, FuzzyScore(query, t))
	}
	return best
}

// RankSouls filters souls to those scoring > 0 for query and sorts them by
// score (descending), like the website's gallery while searching. Ties — and
// the everything-matches empty query — fall back to name ascending, the
// site's default sort.
func RankSouls(query string, souls []api.SoulCard) []api.SoulCard {
	return rank(query, souls, ScoreSoul, func(s api.SoulCard) string { return s.Name })
}

// RankListings filters listings to those scoring > 0 for query and sorts them
// by score (descending), ties broken by name ascending.
func RankListings(query string, listings []api.Listing) []api.Listing {
	return rank(query, listings, ScoreListing, func(l api.Listing) string { return l.Name })
}

// rank scores, filters and sorts one slice of items: score descending, then
// name ascending (case-insensitive), preserving input order on full ties.
func rank[T any](query string, items []T, score func(string, T) int, name func(T) string) []T {
	type scored struct {
		item  T
		score int
	}
	kept := make([]scored, 0, len(items))
	for _, it := range items {
		if s := score(query, it); s > 0 {
			kept = append(kept, scored{item: it, score: s})
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].score != kept[j].score {
			return kept[i].score > kept[j].score
		}
		a, b := strings.ToLower(name(kept[i].item)), strings.ToLower(name(kept[j].item))
		if a != b {
			return a < b
		}
		return false
	})

	out := make([]T, len(kept))
	for i, k := range kept {
		out[i] = k.item
	}
	return out
}
