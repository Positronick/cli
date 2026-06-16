package api

import (
	"context"
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
