package mcpserver

import (
	"context"
	"errors"
	"testing"
)

// notFoundErr is the shared builder behind soul_show/soul_install and
// listing_show's "not found" tool errors. Its contract: always name the
// missing noun+slug and point at the search tool; add a did-you-mean only
// when a suggester is wired AND finds a neighbor; and never let a failing
// suggestion fetch mask the original not-found.
func TestNotFoundErr(t *testing.T) {
	okFetch := func(context.Context) ([]string, error) { return []string{"agentloop", "agentflow"}, nil }
	failFetch := func(context.Context) ([]string, error) { return nil, errors.New("network down") }
	suggestFirst := func(_ string, candidates []string) string { return candidates[0] }
	suggestNone := func(string, []string) string { return "" }

	tests := []struct {
		name  string
		opts  Options
		noun  string
		fetch func(context.Context) ([]string, error)
		want  string
	}{
		{
			name: "no suggester falls back to browse hint",
			opts: Options{Suggest: nil},
			noun: "soul", fetch: okFetch,
			want: `soul "agntloop" not found (browse with soul_search)`,
		},
		{
			name: "suggester with a match yields did-you-mean",
			opts: Options{Suggest: suggestFirst},
			noun: "listing", fetch: okFetch,
			want: `listing "agntloop" not found — did you mean "agentloop"? (browse with listing_search)`,
		},
		{
			name: "suggester with no match falls back to browse hint",
			opts: Options{Suggest: suggestNone},
			noun: "soul", fetch: okFetch,
			want: `soul "agntloop" not found (browse with soul_search)`,
		},
		{
			name: "a failing candidate fetch never masks the not-found",
			opts: Options{Suggest: suggestFirst},
			noun: "listing", fetch: failFetch,
			want: `listing "agntloop" not found (browse with listing_search)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := notFoundErr(context.Background(), tt.opts, "agntloop", tt.noun, tt.fetch)
			if err == nil {
				t.Fatalf("notFoundErr returned nil, want %q", tt.want)
			}
			if got := err.Error(); got != tt.want {
				t.Errorf("notFoundErr = %q, want %q", got, tt.want)
			}
		})
	}
}
