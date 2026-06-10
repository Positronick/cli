package api

import (
	"context"
	"net/http"
)

// User is the authenticated identity returned by GET /api/me. Image and
// GithubLogin are nullable (e.g. a Google-only account has no GitHub login),
// so they are pointers and null round-trips as null in --json output.
type User struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Email       string  `json:"email"`
	Image       *string `json:"image"`
	GithubLogin *string `json:"githubLogin"`
}

// Me is the GET /api/me response. IsAdmin is computed server-side; the CLI
// caches it but never decides it.
type Me struct {
	User    User `json:"user"`
	IsAdmin bool `json:"isAdmin"`
}

// APIKey is the POST /api/auth/api-key/create response. Key is the raw secret
// — the server stores only a hash, so this is the one and only time it is
// visible. ExpiresAt stays an ISO-8601 string (or null for no expiry).
type APIKey struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Start     *string `json:"start"`
	Prefix    *string `json:"prefix"`
	Key       string  `json:"key"`
	ExpiresAt *string `json:"expiresAt"`
}

// Me fetches the authenticated identity: GET /api/me. Without valid
// credentials the server answers 401, surfaced as an *APIError for which
// IsAuthError is true.
func (c *Client) Me(ctx context.Context) (*Me, error) {
	var out Me
	if err := c.do(ctx, http.MethodGet, "/api/me", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// createAPIKeyRequest is the POST /api/auth/api-key/create body. ExpiresIn is
// in seconds and omitted when zero so the server applies its default expiry.
type createAPIKeyRequest struct {
	Name      string `json:"name"`
	ExpiresIn int    `json:"expiresIn,omitempty"`
}

// CreateAPIKey mints a new API key: POST /api/auth/api-key/create. The server
// only allows session (bearer) credentials to mint keys — an API key cannot
// create more API keys. expiresIn is in seconds; 0 means the server default.
func (c *Client) CreateAPIKey(ctx context.Context, name string, expiresIn int) (*APIKey, error) {
	var out APIKey
	in := createAPIKeyRequest{Name: name, ExpiresIn: expiresIn}
	if err := c.do(ctx, http.MethodPost, "/api/auth/api-key/create", nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
