package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// This file is the typed client for the public "what's new" read endpoint
// (GET /api/research). Unlike the soul/listing galleries — which reserve ?q=
// and filter client-side — the research endpoint filters server-side, so the
// query params travel on the wire. It is unauthenticated, read-only, and only
// ever returns published posts.

// ResearchQuery carries the optional filters for GET /api/research. The zero
// value (all fields empty, Limit 0) fetches the default feed: every published
// post, newest first, up to the server's default page size.
type ResearchQuery struct {
	// Q is a free-text query over title/excerpt.
	Q string
	// Kind narrows to one of PostKinds (article, release, link).
	Kind string
	// Category narrows to one blog category.
	Category string
	// Tag narrows to posts carrying this tag.
	Tag string
	// Since is an ISO-8601 instant; only posts published strictly after it are
	// returned (the delta since a previous poll).
	Since string
	// Limit caps the result count (server range 1–100). 0 omits the param and
	// takes the server default.
	Limit int
}

// ResearchResult is the GET /api/research response: the matched items plus
// Latest, the newest publishedAt within the same filter (kind/category/tag/q),
// ignoring Since. Feed Latest back as the next Since to poll only the delta;
// it is null when the filter matched nothing at all.
type ResearchResult struct {
	Results []ResearchItem `json:"results"`
	Latest  *string        `json:"latest"`
}

// Research fetches the "what's new" feed: GET /api/research. The server
// validates the params and answers 400 invalid_input (surfaced verbatim) on a
// bad kind, limit, or since value.
func (c *Client) Research(ctx context.Context, q ResearchQuery) (*ResearchResult, error) {
	query := url.Values{}
	for key, val := range map[string]string{
		"q":        q.Q,
		"kind":     q.Kind,
		"category": q.Category,
		"tag":      q.Tag,
		"since":    q.Since,
	} {
		if val != "" {
			query.Set(key, val)
		}
	}
	if q.Limit > 0 {
		query.Set("limit", strconv.Itoa(q.Limit))
	}

	var out ResearchResult
	if err := c.do(ctx, http.MethodGet, "/api/research", query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
