package search

import (
	"testing"

	"github.com/positronick/cli/internal/api"
)

// These tests port the actual vectors from the website's vitest suites
// (src/lib/filter.test.ts and src/lib/listingFilter.test.ts in the product
// repo). Ranking parity with positronick.com is a product guarantee: a search
// in the CLI must surface the same results in the same order as the gallery.
// Each test cites the source test name it ports.

// makeSoul mirrors filter.test.ts's makeSoul helper: sensible defaults,
// override only what a test cares about.
func makeSoul(over api.SoulCard) api.SoulCard {
	s := api.SoulCard{
		ID:           "id-x",
		Slug:         "slug",
		SlugHistory:  []string{},
		Name:         "Name",
		AuthorHandle: "nick",
		Tagline:      "A tagline",
		Category:     "Technical",
		Tags:         []string{},
		Frameworks:   []string{"hermes"},
		Models:       []string{},
		Version:      "1.0.0",
		License:      "MIT",
		ContentHash:  "hash",
		Status:       "published",
		CreatedAt:    "2026-01-01T00:00:00.000Z",
		UpdatedAt:    "2026-01-01T00:00:00.000Z",
	}
	if over.Slug != "" {
		s.Slug = over.Slug
		s.ID = "id-" + over.Slug
	}
	if over.Name != "" {
		s.Name = over.Name
	}
	if over.Tagline != "" {
		s.Tagline = over.Tagline
	}
	if over.Tags != nil {
		s.Tags = over.Tags
	}
	if over.Frameworks != nil {
		s.Frameworks = over.Frameworks
	}
	return s
}

// makeListing mirrors listingFilter.test.ts's listing() helper.
func makeListing(over api.Listing) api.Listing {
	l := api.Listing{
		ID:            "id",
		Slug:          "slug",
		ProfileHandle: "acme",
		ProfileName:   "Acme",
		Name:          "Thing",
		Type:          "cli",
		Category:      "DevOps",
		Tags:          []string{},
		Official:      true,
		SourceURL:     "https://example.com",
		Data:          map[string]any{},
		Confidence:    "high",
		Status:        "published",
		CreatedAt:     "2026-01-01T00:00:00.000Z",
		UpdatedAt:     "2026-01-01T00:00:00.000Z",
	}
	if over.Name != "" {
		l.Name = over.Name
	}
	if over.Type != "" {
		l.Type = over.Type
	}
	if over.Tagline != "" {
		l.Tagline = over.Tagline
	}
	if over.Description != nil {
		l.Description = over.Description
	}
	if over.Category != "" {
		l.Category = over.Category
	}
	if over.ProfileHandle != "" {
		l.ProfileHandle = over.ProfileHandle
	}
	return l
}

func ptr(s string) *string { return &s }

// Ports filter.test.ts "fuzzyScore — why: search must rank exact/contiguous
// hits above loose subsequence hits".
func TestFuzzyScore(t *testing.T) {
	// filter.test.ts: "treats an empty needle as a universal match"
	t.Run("empty needle is a universal match", func(t *testing.T) {
		if got := FuzzyScore("", "anything"); got <= 0 {
			t.Errorf("FuzzyScore(\"\", \"anything\") = %d, want > 0", got)
		}
	})

	// filter.test.ts: "scores a contiguous substring higher than a scattered subsequence"
	t.Run("contiguous substring beats scattered subsequence", func(t *testing.T) {
		contiguous := FuzzyScore("back", "backend engineer")
		scattered := FuzzyScore("back", "b a c k") // subsequence, not contiguous
		if contiguous <= scattered {
			t.Errorf("contiguous = %d, scattered = %d; want contiguous > scattered", contiguous, scattered)
		}
		if scattered <= 0 {
			t.Errorf("scattered = %d, want > 0", scattered)
		}
	})

	// filter.test.ts: "returns 0 when the needle is not even a subsequence"
	t.Run("non-subsequence scores zero", func(t *testing.T) {
		if got := FuzzyScore("xyz", "backend"); got != 0 {
			t.Errorf("FuzzyScore(\"xyz\", \"backend\") = %d, want 0", got)
		}
	})

	// filter.test.ts: "rewards an earlier substring match over a later one"
	t.Run("earlier substring match beats later one", func(t *testing.T) {
		early := FuzzyScore("eng", "engineer")
		late := FuzzyScore("eng", "senior engineer")
		if early <= late {
			t.Errorf("early = %d, late = %d; want early > late", early, late)
		}
	})
}

// Pins the exact numeric semantics ported from filter.ts: contiguous match =
// 2000 - index; subsequence = +5 per adjacent char, +1 otherwise. These exact
// values are what makes CLI ranking identical to the website's.
func TestFuzzyScoreNumericParity(t *testing.T) {
	tests := []struct {
		name     string
		needle   string
		haystack string
		want     int
	}{
		{"contiguous at index 0", "back", "backend engineer", 2000},
		{"contiguous at index 7", "eng", "senior engineer", 1993},
		{"scattered subsequence scores 1 per char", "back", "b a c k", 4},
		{"adjacent subsequence chars score 5", "abc", "a x bc", 7}, // a@0 (+1), b@4 (+1), c@5 adjacent (+5)
		{"case-insensitive", "BACK", "Backend", 2000},
		{"needle is trimmed", "  back  ", "backend", 2000},
		{"empty needle scores 1", "", "anything", 1},
		{"missing char scores 0", "aa", "a", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FuzzyScore(tt.needle, tt.haystack); got != tt.want {
				t.Errorf("FuzzyScore(%q, %q) = %d, want %d", tt.needle, tt.haystack, got, tt.want)
			}
		})
	}
}

// Souls fixture ported from filter.test.ts "filterSouls — why: the gallery
// must filter/sort exactly as a user expects".
func soulsFixture() []api.SoulCard {
	return []api.SoulCard{
		makeSoul(api.SoulCard{
			Slug:       "a",
			Name:       "Backend Engineer",
			Tags:       []string{"backend", "api"},
			Frameworks: []string{"hermes"},
		}),
		makeSoul(api.SoulCard{
			Slug:       "b",
			Name:       "Mindful Companion",
			Tags:       []string{"calm"},
			Frameworks: []string{"claude-code"},
		}),
		makeSoul(api.SoulCard{
			Slug:       "c",
			Name:       "Dungeon Master",
			Tags:       []string{"rpg", "fun"},
			Frameworks: []string{"hermes", "cursor"},
		}),
	}
}

func slugs(souls []api.SoulCard) []string {
	out := make([]string, len(souls))
	for i, s := range souls {
		out[i] = s.Slug
	}
	return out
}

func names(ls []api.Listing) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Name
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRankSouls(t *testing.T) {
	souls := soulsFixture()

	// filter.test.ts: "matches free text against name, tagline and tags"
	t.Run("matches free text against name and tags", func(t *testing.T) {
		if got := slugs(RankSouls("backend", souls)); !equal(got, []string{"a"}) {
			t.Errorf("RankSouls(\"backend\") = %v, want [a]", got)
		}
		if got := slugs(RankSouls("rpg", souls)); !equal(got, []string{"c"}) {
			t.Errorf("RankSouls(\"rpg\") = %v, want [c]", got)
		}
	})

	// filter.test.ts: "drops non-matches entirely when searching"
	t.Run("drops non-matches entirely when searching", func(t *testing.T) {
		if got := RankSouls("zzzzz", souls); len(got) != 0 {
			t.Errorf("RankSouls(\"zzzzz\") = %v, want empty", slugs(got))
		}
	})

	// filter.test.ts: "returns all souls (name-sorted) for the default empty query"
	t.Run("empty query returns all souls name-sorted", func(t *testing.T) {
		got := slugs(RankSouls("", souls))
		// Backend Engineer, Dungeon Master, Mindful Companion
		if !equal(got, []string{"a", "c", "b"}) {
			t.Errorf("RankSouls(\"\") = %v, want [a c b]", got)
		}
	})

	// Frameworks are searchable (filter.ts relevance() spreads soul.frameworks).
	t.Run("matches frameworks in free text", func(t *testing.T) {
		got := slugs(RankSouls("claude-code", souls))
		if len(got) == 0 || got[0] != "b" {
			t.Errorf("RankSouls(\"claude-code\") = %v, want b first", got)
		}
	})
}

// Listings fixture ported from listingFilter.test.ts.
func listingsFixture() []api.Listing {
	return []api.Listing{
		makeListing(api.Listing{Name: "Playwright MCP", Type: "mcp", Category: "Web", ProfileHandle: "microsoft"}),
		makeListing(api.Listing{Name: "GitHub CLI", Type: "cli", Category: "DevOps", ProfileHandle: "github"}),
		makeListing(api.Listing{Name: "Claude Code", Type: "cli", Category: "AI/ML", ProfileHandle: "anthropic"}),
	}
}

func TestRankListings(t *testing.T) {
	items := listingsFixture()

	// listingFilter.test.ts: "matches the author handle in free-text search"
	t.Run("matches the author handle in free-text search", func(t *testing.T) {
		got := RankListings("github", items)
		if len(got) == 0 || got[0].Name != "GitHub CLI" {
			t.Errorf("RankListings(\"github\")[0] = %v, want GitHub CLI", names(got))
		}
	})

	// listingFilter.test.ts: "finds a listing by a term that appears only in its
	// description" — why: descriptions carry the richest "what it does / when to
	// use it" signal, so a user searching a concept that isn't in the
	// name/tagline should still surface the tool.
	t.Run("finds a listing by a term only in its description", func(t *testing.T) {
		withDesc := []api.Listing{
			makeListing(api.Listing{Name: "Wrangler", Tagline: "CLI for the edge"}),
			makeListing(api.Listing{
				Name:        "mcp-to-ai-sdk",
				Tagline:     "Generate tool stubs",
				Description: ptr("Cuts token cost and reduces prompt-injection risk from MCP servers."),
			}),
		}
		got := names(RankListings("prompt-injection", withDesc))
		if !equal(got, []string{"mcp-to-ai-sdk"}) {
			t.Errorf("RankListings(\"prompt-injection\") = %v, want [mcp-to-ai-sdk]", got)
		}
	})

	// listingFilter.test.ts: "sorts A–Z by default"
	t.Run("empty query sorts A-Z", func(t *testing.T) {
		got := names(RankListings("", items))
		if !equal(got, []string{"Claude Code", "GitHub CLI", "Playwright MCP"}) {
			t.Errorf("RankListings(\"\") = %v, want A-Z", got)
		}
	})

	// The type field is searchable (listingFilter.ts relevance() includes l.type).
	t.Run("matches the listing type in free text", func(t *testing.T) {
		got := RankListings("mcp", items)
		if len(got) == 0 {
			t.Fatal("RankListings(\"mcp\") returned no results")
		}
		// "Playwright MCP" has the contiguous name hit (weighted x3) and must
		// outrank "Claude Code"/"GitHub CLI", whose only hit is type/tagline.
		if got[0].Name != "Playwright MCP" {
			t.Errorf("RankListings(\"mcp\")[0] = %q, want Playwright MCP", got[0].Name)
		}
	})
}

// ScoreSoul applies the exact field weights from filter.ts relevance():
// name x3, tagline x2, authorHandle x1, tags and frameworks unweighted.
func TestScoreSoulFieldWeights(t *testing.T) {
	s := makeSoul(api.SoulCard{Slug: "w", Name: "alpha", Tagline: "alpha", Tags: []string{"alpha"}})
	// name hit: 2000*3; tagline hit: 2000*2; tag hit: 2000*1 — max wins.
	if got := ScoreSoul("alpha", s); got != 6000 {
		t.Errorf("ScoreSoul name-weight = %d, want 6000 (2000 x 3)", got)
	}

	tagOnly := makeSoul(api.SoulCard{Slug: "t", Name: "zzz", Tagline: "zzz", Tags: []string{"alpha"}})
	if got := ScoreSoul("alpha", tagOnly); got != 2000 {
		t.Errorf("ScoreSoul tag-weight = %d, want 2000 (unweighted)", got)
	}

	if got := ScoreSoul("", s); got != 1 {
		t.Errorf("ScoreSoul empty query = %d, want 1", got)
	}
}

// ScoreListing applies the exact field weights from listingFilter.ts
// relevance(): name x3, tagline x2, profileHandle x2, profileName, type,
// description, tags unweighted.
func TestScoreListingFieldWeights(t *testing.T) {
	l := makeListing(api.Listing{Name: "alpha", ProfileHandle: "alpha"})
	if got := ScoreListing("alpha", l); got != 6000 {
		t.Errorf("ScoreListing name-weight = %d, want 6000 (2000 x 3)", got)
	}

	handleOnly := makeListing(api.Listing{Name: "zzz", ProfileHandle: "alpha"})
	if got := ScoreListing("alpha", handleOnly); got != 4000 {
		t.Errorf("ScoreListing profileHandle-weight = %d, want 4000 (2000 x 2)", got)
	}

	if got := ScoreListing("", l); got != 1 {
		t.Errorf("ScoreListing empty query = %d, want 1", got)
	}

	// nil description must score like the website's `l.description ?? ''`.
	noDesc := makeListing(api.Listing{Name: "zzz"})
	if got := ScoreListing("anything", noDesc); got != 0 {
		t.Errorf("ScoreListing nil-description = %d, want 0", got)
	}
}
