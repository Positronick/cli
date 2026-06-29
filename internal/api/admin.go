package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

// This file is the typed client for the admin write API (/api/admin/*),
// implemented server-side in the product repo's admin routes. Requests
// authenticate through the client's normal credential chain; the server
// answers 401/403 for non-admins. There is deliberately NO delete method:
// the server has no DELETE verb — unpublishing is a PATCH to status "draft".

// AdminSoul is a soul as the admin API returns it: the public shape plus
// Source, the ownership marker ("seed" = owned by the git content pipeline,
// "api" = owned by the write API; the seed skips api-owned rows).
type AdminSoul struct {
	Soul
	Source string `json:"source"`
}

// AdminListing is a listing as the admin API returns it, plus Source (same
// semantics as AdminSoul.Source).
type AdminListing struct {
	Listing
	Source string `json:"source"`
}

// AdminProfile is a profile as the admin API returns it, plus Source (same
// semantics as AdminSoul.Source: "seed" = git-curated, "api" = admin-created).
type AdminProfile struct {
	Profile
	Source string `json:"source"`
}

// AdminPost is a blog post as the admin API returns it: the full public Post
// (markdown body included) plus Source, the ownership marker. For posts the
// values are "feed" (mirrored from a feed source by the ingestor, which keeps
// it fresh) and "api" (authored or edited through the write API, which the
// ingestor leaves alone). Editing a feed-owned post flips it to api, reported
// as tookOwnership.
type AdminPost struct {
	Post
	Source string `json:"source"`
}

// CreateSoul creates a soul: POST /api/admin/souls. fields is sent verbatim
// as the JSON body — the server validates with the seed's own validator and
// answers 422 with the validator message, 409 on a slug conflict. The id is
// server-assigned; the server rejects a client-supplied one.
func (c *Client) CreateSoul(ctx context.Context, fields map[string]any) (*AdminSoul, error) {
	var out struct {
		Soul AdminSoul `json:"soul"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/admin/souls", nil, fields, &out); err != nil {
		return nil, err
	}
	return &out.Soul, nil
}

// AdminSoul fetches one soul by id, any status: GET /api/admin/souls/{id}.
func (c *Client) AdminSoul(ctx context.Context, id string) (*AdminSoul, error) {
	var out struct {
		Soul AdminSoul `json:"soul"`
	}
	path := "/api/admin/souls/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out.Soul, nil
}

// UpdateSoul patches a soul: PATCH /api/admin/souls/{id}. patch carries only
// the fields to change. tookOwnership is true when the row was seed-owned and
// this update flipped it to api-owned — the caller must warn that the git
// copy is now inert and will be skipped by deploys.
func (c *Client) UpdateSoul(ctx context.Context, id string, patch map[string]any) (*AdminSoul, bool, error) {
	var out struct {
		Soul          AdminSoul `json:"soul"`
		TookOwnership bool      `json:"tookOwnership"`
	}
	path := "/api/admin/souls/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodPatch, path, nil, patch, &out); err != nil {
		return nil, false, err
	}
	return &out.Soul, out.TookOwnership, nil
}

// CreateListing creates a registry listing: POST /api/admin/listings. The
// authoring profile is referenced by handle (profileHandle) and must already
// exist — profiles stay git-curated; the server answers 422 unknown_profile
// otherwise.
func (c *Client) CreateListing(ctx context.Context, fields map[string]any) (*AdminListing, error) {
	var out struct {
		Listing AdminListing `json:"listing"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/admin/listings", nil, fields, &out); err != nil {
		return nil, err
	}
	return &out.Listing, nil
}

// AdminListing fetches one listing by id, any status: GET /api/admin/listings/{id}.
func (c *Client) AdminListing(ctx context.Context, id string) (*AdminListing, error) {
	var out struct {
		Listing AdminListing `json:"listing"`
	}
	path := "/api/admin/listings/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out.Listing, nil
}

// UpdateListing patches a listing: PATCH /api/admin/listings/{id}. A
// profileHandle in the patch is re-resolved server-side. tookOwnership has
// the same semantics as UpdateSoul.
func (c *Client) UpdateListing(ctx context.Context, id string, patch map[string]any) (*AdminListing, bool, error) {
	var out struct {
		Listing       AdminListing `json:"listing"`
		TookOwnership bool         `json:"tookOwnership"`
	}
	path := "/api/admin/listings/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodPatch, path, nil, patch, &out); err != nil {
		return nil, false, err
	}
	return &out.Listing, out.TookOwnership, nil
}

// CreateProfile creates a registry profile: POST /api/admin/profiles. fields is
// sent verbatim as the JSON body — the server validates it and answers 422 on a
// bad field, 409 when the handle is already taken. The id and source are
// server-assigned (source becomes "api"); verified/official default false.
// Profiles were historically git-curated only; this is the admin write path.
func (c *Client) CreateProfile(ctx context.Context, fields map[string]any) (*AdminProfile, error) {
	var out struct {
		Profile AdminProfile `json:"profile"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/admin/profiles", nil, fields, &out); err != nil {
		return nil, err
	}
	return &out.Profile, nil
}

// Profiles lists every profile (any source): GET /api/admin/profiles. Admin
// only — the server answers 401/403 for non-admins. Useful for discovering
// which handles already exist before authoring a listing.
func (c *Client) Profiles(ctx context.Context) ([]AdminProfile, error) {
	var out struct {
		Profiles []AdminProfile `json:"profiles"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/admin/profiles", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Profiles, nil
}

// Feeds lists every blog feed source, newest first: GET /api/admin/feeds.
// Admin only — the server answers 401/403 for non-admins.
func (c *Client) Feeds(ctx context.Context) ([]FeedSource, error) {
	var out struct {
		Feeds []FeedSource `json:"feeds"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/admin/feeds", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Feeds, nil
}

// CreateFeed creates a blog feed source: POST /api/admin/feeds. fields is sent
// verbatim as the JSON body — the server validates it and answers 422 with the
// validator message, 422 unknown_profile when authorHandle resolves to no
// profile, or 422 invalid_input when listingSlug resolves to no listing. The id
// and source are server-assigned (source is always "api").
func (c *Client) CreateFeed(ctx context.Context, fields map[string]any) (*FeedSource, error) {
	var out struct {
		Feed FeedSource `json:"feed"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/admin/feeds", nil, fields, &out); err != nil {
		return nil, err
	}
	return &out.Feed, nil
}

// AdminFeed fetches one feed source by id: GET /api/admin/feeds/{id}.
func (c *Client) AdminFeed(ctx context.Context, id string) (*FeedSource, error) {
	var out struct {
		Feed FeedSource `json:"feed"`
	}
	path := "/api/admin/feeds/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out.Feed, nil
}

// UpdateFeed patches a feed source: PATCH /api/admin/feeds/{id}. patch carries
// only the fields to change — {"enabled":false} pauses a feed (there is no
// delete verb). Feed sources carry no seed/api ownership flip, so unlike soul
// and listing updates there is no tookOwnership signal.
func (c *Client) UpdateFeed(ctx context.Context, id string, patch map[string]any) (*FeedSource, error) {
	var out struct {
		Feed FeedSource `json:"feed"`
	}
	path := "/api/admin/feeds/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodPatch, path, nil, patch, &out); err != nil {
		return nil, err
	}
	return &out.Feed, nil
}

// SyncFeed ingests one feed now: POST /api/admin/feeds/{id}/sync. It returns
// the ingest summary on success AND on a fetch/parse failure: the server
// answers 502 with the same {"summary":{...}} body (not the error envelope)
// when the fetch/parse failed, and that summary's Error field carries the
// reason. The caller surfaces a non-empty Error as a clear failure. A missing
// feed is the usual 404 *APIError.
func (c *Client) SyncFeed(ctx context.Context, id string) (*FeedSyncSummary, error) {
	var out struct {
		Summary FeedSyncSummary `json:"summary"`
	}
	path := "/api/admin/feeds/" + url.PathEscape(id) + "/sync"
	err := c.do(ctx, http.MethodPost, path, nil, nil, &out)
	if err == nil {
		return &out.Summary, nil
	}
	// The 502 fetch/parse-failure response is {"summary":{...}}, not the error
	// envelope, so do() turned it into a bare *APIError — recover the summary
	// (with its Error reason) from the captured body so the caller can surface
	// why the fetch failed instead of a generic "Bad Gateway".
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusBadGateway {
		var body struct {
			Summary FeedSyncSummary `json:"summary"`
		}
		if json.Unmarshal(apiErr.Body, &body) == nil && body.Summary.Error != "" {
			return &body.Summary, nil
		}
	}
	return nil, err
}

// CreatePost creates a blog post: POST /api/admin/posts. fields is sent
// verbatim as the JSON body — the server validates it and answers 422 with the
// validator message, 409 on a slug conflict. The id is server-assigned (the
// server rejects a client-supplied one) and status defaults to "draft": an
// agent-authored post cannot self-publish, it must be promoted deliberately.
func (c *Client) CreatePost(ctx context.Context, fields map[string]any) (*AdminPost, error) {
	var out struct {
		Post AdminPost `json:"post"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/admin/posts", nil, fields, &out); err != nil {
		return nil, err
	}
	return &out.Post, nil
}

// AdminPost fetches one post by id, any status: GET /api/admin/posts/{id}.
func (c *Client) AdminPost(ctx context.Context, id string) (*AdminPost, error) {
	var out struct {
		Post AdminPost `json:"post"`
	}
	path := "/api/admin/posts/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out.Post, nil
}

// AdminPosts lists every post (any status, source included): GET
// /api/admin/posts. Admin only — the server answers 401/403 for non-admins.
// Named AdminPosts (not Posts) to leave the public, published-only blog gallery
// reader Posts(ctx, kind) untouched.
func (c *Client) AdminPosts(ctx context.Context) ([]AdminPost, error) {
	var out struct {
		Posts []AdminPost `json:"posts"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/admin/posts", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Posts, nil
}

// UpdatePost patches a post: PATCH /api/admin/posts/{id}. patch carries only
// the fields to change — {"status":"draft"} unpublishes (there is no delete
// verb). tookOwnership is true when the row was feed-owned and this edit flipped
// it to api-owned: the ingestor will no longer refresh it from its source.
func (c *Client) UpdatePost(ctx context.Context, id string, patch map[string]any) (*AdminPost, bool, error) {
	var out struct {
		Post          AdminPost `json:"post"`
		TookOwnership bool      `json:"tookOwnership"`
	}
	path := "/api/admin/posts/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodPatch, path, nil, patch, &out); err != nil {
		return nil, false, err
	}
	return &out.Post, out.TookOwnership, nil
}
