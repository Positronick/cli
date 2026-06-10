package api

import (
	"context"
	"net/http"
	"net/url"
)

// Listings fetches registry listings: GET /api/listings. An empty listingType
// returns all listings; otherwise ?type= is sent and the server rejects
// unknown types with an invalid_type error.
func (c *Client) Listings(ctx context.Context, listingType string) ([]Listing, error) {
	var query url.Values
	if listingType != "" {
		query = url.Values{"type": {listingType}}
	}
	var out struct {
		Listings []Listing `json:"listings"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/listings", query, nil, &out); err != nil {
		return nil, err
	}
	return out.Listings, nil
}

// Listing fetches one registry listing: GET /api/listings/{slug}.
func (c *Client) Listing(ctx context.Context, slug string) (*Listing, error) {
	var out struct {
		Listing Listing `json:"listing"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/listings/"+url.PathEscape(slug), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out.Listing, nil
}
