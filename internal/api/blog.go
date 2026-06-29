package api

import (
	"context"
	"net/http"
	"net/url"
)

// This file is the typed client for the public blog read endpoints
// (GET /api/blog, /api/blog/{slug}, /api/blog/{slug}.md) — the soul/listing
// read twins for editorial posts, mirrored GitHub releases, and mirrored news
// links. All three are unauthenticated, read-only, and only ever return
// published posts. Like the soul gallery, the list reserves ?q= and filters
// client-side; only ?kind= travels on the wire.

// Posts fetches the published blog gallery, newest first: GET /api/blog. A
// non-empty kind narrows to one of PostKinds (article, release, link) via the
// server's ?kind= filter; "" fetches every kind.
func (c *Client) Posts(ctx context.Context, kind string) ([]PostCard, error) {
	query := url.Values{}
	if kind != "" {
		query.Set("kind", kind)
	}
	var out struct {
		Posts []PostCard `json:"posts"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/blog", query, nil, &out); err != nil {
		return nil, err
	}
	return out.Posts, nil
}

// Post fetches one post with its markdown body: GET /api/blog/{slug}. A renamed
// slug is followed via the server's 301; a missing one surfaces as an *APIError
// for which IsNotFound is true. This endpoint never bumps the post's viewCount.
func (c *Client) Post(ctx context.Context, slug string) (*Post, error) {
	var out struct {
		Post Post `json:"post"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/blog/"+url.PathEscape(slug), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out.Post, nil
}

// PostMarkdown fetches the raw post markdown verbatim: GET /api/blog/{slug}.md.
// Unlike the soul .md endpoint (which bumps the install counter), the blog .md
// endpoint never bumps viewCount — counting stays exclusive to the HTML page
// view — so `blog show --raw` reads it directly. A renamed slug is followed via
// the server's 301; a missing one is an IsNotFound *APIError.
func (c *Client) PostMarkdown(ctx context.Context, slug string) (string, error) {
	var body string
	if err := c.do(ctx, http.MethodGet, "/api/blog/"+url.PathEscape(slug)+".md", nil, nil, &body); err != nil {
		return "", err
	}
	return body, nil
}
