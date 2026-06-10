package cli

import "github.com/positronick/cli/internal/search"

// maxSuggestionDistance is the largest edit distance still considered a typo
// of an existing slug.
const maxSuggestionDistance = 3

// levenshtein returns the edit distance between a and b, counted in runes so
// multi-byte characters cost one edit, not several.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// closestSlug picks the did-you-mean candidate for input: the candidate with
// the smallest Levenshtein distance when that distance is at most 3 (typos),
// otherwise the best fuzzy-score candidate (fragments like "sher"), otherwise
// "" — no suggestion beats a misleading one. Earlier candidates win exact
// ties so the suggestion is deterministic.
func closestSlug(input string, candidates []string) string {
	best, bestDist := "", maxSuggestionDistance+1
	for _, c := range candidates {
		if d := levenshtein(input, c); d < bestDist {
			best, bestDist = c, d
		}
	}
	if best != "" {
		return best
	}

	bestScore := 0
	for _, c := range candidates {
		if s := search.FuzzyScore(input, c); s > bestScore {
			best, bestScore = c, s
		}
	}
	return best
}
