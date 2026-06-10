package api

import (
	"context"
	"net/http"
	"net/url"
)

// Souls fetches the full soul gallery: GET /api/souls.
func (c *Client) Souls(ctx context.Context) ([]SoulCard, error) {
	var out struct {
		Souls []SoulCard `json:"souls"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/souls", nil, &out); err != nil {
		return nil, err
	}
	return out.Souls, nil
}

// Soul fetches one soul with its markdown body: GET /api/souls/{slug}.
// A renamed slug is followed via the server's 301; a missing one surfaces as
// an *APIError for which IsNotFound is true.
func (c *Client) Soul(ctx context.Context, slug string) (*Soul, error) {
	var out struct {
		Soul Soul `json:"soul"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/souls/"+url.PathEscape(slug), nil, &out); err != nil {
		return nil, err
	}
	return &out.Soul, nil
}

// SoulMarkdown fetches the raw SOUL.md body verbatim: GET /api/souls/{slug}.md.
// This is the install contract — the returned string is byte-identical to
// what `curl .../api/souls/{slug}.md` writes, so installs are lossless.
func (c *Client) SoulMarkdown(ctx context.Context, slug string) (string, error) {
	var body string
	if err := c.do(ctx, http.MethodGet, "/api/souls/"+url.PathEscape(slug)+".md", nil, &body); err != nil {
		return "", err
	}
	return body, nil
}
