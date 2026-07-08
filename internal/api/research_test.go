package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Research maps its query fields to the documented wire params, omits empty
// ones (and a zero Limit), and decodes the {results, latest} envelope.
func TestResearchQueryParams(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		if r.URL.Path != "/api/research" {
			t.Errorf("path = %q, want /api/research", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"results":[{"slug":"hermes-v2-1-0","kind":"release"}],"latest":"2026-06-01T09:00:00.000Z"}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Anonymous{})

	// Zero value sends no query string at all.
	if _, err := c.Research(context.Background(), ResearchQuery{}); err != nil {
		t.Fatalf("Research(zero): %v", err)
	}
	if q := rec.last().URL.RawQuery; q != "" {
		t.Errorf("zero query must send no query string, got %q", q)
	}

	res, err := c.Research(context.Background(), ResearchQuery{
		Q: "hermes", Kind: "release", Category: "Releases", Tag: "hermes",
		Since: "2026-05-01T00:00:00.000Z", Limit: 5,
	})
	if err != nil {
		t.Fatalf("Research(full): %v", err)
	}
	got := rec.last().URL.Query()
	for key, want := range map[string]string{
		"q": "hermes", "kind": "release", "category": "Releases",
		"tag": "hermes", "since": "2026-05-01T00:00:00.000Z", "limit": "5",
	} {
		if got.Get(key) != want {
			t.Errorf("query %q = %q, want %q", key, got.Get(key), want)
		}
	}

	if res.Latest == nil || *res.Latest != "2026-06-01T09:00:00.000Z" {
		t.Errorf("latest = %v, want the decoded high-water mark", res.Latest)
	}
	if len(res.Results) != 1 || res.Results[0].Slug != "hermes-v2-1-0" {
		t.Errorf("results = %+v, want the decoded item", res.Results)
	}
}
