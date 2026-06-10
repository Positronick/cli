package cli

import "testing"

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"sherlock", "sherlock", 0},
		{"sherloc", "sherlock", 1},
		{"watsno", "watson", 2},
		{"kitten", "sitting", 3},
		{"", "abc", 3},
		{"abc", "", 3},
		{"caffè", "caffe", 1}, // rune-based, not byte-based
	}
	for _, tt := range tests {
		t.Run(tt.a+"/"+tt.b, func(t *testing.T) {
			if got := levenshtein(tt.a, tt.b); got != tt.want {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// did-you-mean drives the exit-3 hint: typos resolve via edit distance ≤ 3,
// fragments via the site's fuzzy score, and garbage suggests nothing rather
// than something misleading.
func TestClosestSlug(t *testing.T) {
	slugs := []string{"sherlock", "watson", "moriarty"}
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"one-letter typo", "sherloc", "sherlock"},
		{"transposition", "watsno", "watson"},
		{"fragment falls back to fuzzy score", "sher", "sherlock"},
		{"no plausible match suggests nothing", "zzz", ""},
		{"exact match wins at distance zero", "moriarty", "moriarty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := closestSlug(tt.input, slugs); got != tt.want {
				t.Errorf("closestSlug(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}

	t.Run("equal distances keep the first candidate (deterministic)", func(t *testing.T) {
		if got := closestSlug("aaaa", []string{"aaab", "aaac"}); got != "aaab" {
			t.Errorf("closestSlug tie = %q, want aaab", got)
		}
	})

	t.Run("no candidates suggests nothing", func(t *testing.T) {
		if got := closestSlug("sherlock", nil); got != "" {
			t.Errorf("closestSlug with no candidates = %q, want empty", got)
		}
	})
}
