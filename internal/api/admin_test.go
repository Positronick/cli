package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// adminEcho captures one admin request and answers with a canned response, so
// each test asserts the exact wire contract: verb, path, JSON body.
type adminEcho struct {
	method string
	path   string
	body   []byte
	status int
	answer string
}

func (e *adminEcho) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.method, e.path = r.Method, r.URL.EscapedPath()
		var err error
		if e.body, err = io.ReadAll(r.Body); err != nil {
			t.Errorf("reading request body: %v", err)
		}
		if len(e.body) > 0 && r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(e.status)
		_, _ = w.Write([]byte(e.answer))
	})
}

func adminClient(t *testing.T, e *adminEcho) *Client {
	t.Helper()
	srv := httptest.NewServer(e.handler(t))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// CreateSoul must POST the field map verbatim to /api/admin/souls and decode
// the created soul including its source — the ownership marker the CLI warns
// about on later updates.
func TestCreateSoul(t *testing.T) {
	e := &adminEcho{status: http.StatusCreated,
		answer: `{"soul":{"id":"01A","slug":"night-owl","name":"Night Owl","source":"api"}}`}
	c := adminClient(t, e)

	soul, err := c.CreateSoul(context.Background(), map[string]any{
		"slug": "night-owl", "name": "Night Owl", "frameworks": []any{"hermes"},
	})
	if err != nil {
		t.Fatalf("CreateSoul: %v", err)
	}
	if e.method != http.MethodPost || e.path != "/api/admin/souls" {
		t.Errorf("request = %s %s, want POST /api/admin/souls", e.method, e.path)
	}
	var sent map[string]any
	if err := json.Unmarshal(e.body, &sent); err != nil {
		t.Fatalf("request body is not JSON: %v (%q)", err, e.body)
	}
	if sent["slug"] != "night-owl" || sent["name"] != "Night Owl" {
		t.Errorf("sent body = %v, want the field map verbatim", sent)
	}
	if soul.ID != "01A" || soul.Slug != "night-owl" || soul.Source != "api" {
		t.Errorf("soul = %+v, want the decoded created soul with source", soul)
	}
}

// AdminSoul must GET /api/admin/souls/{id} — the any-status admin read.
func TestAdminSoul(t *testing.T) {
	e := &adminEcho{status: http.StatusOK,
		answer: `{"soul":{"id":"01A","slug":"night-owl","status":"draft","source":"seed"}}`}
	c := adminClient(t, e)

	soul, err := c.AdminSoul(context.Background(), "01A")
	if err != nil {
		t.Fatalf("AdminSoul: %v", err)
	}
	if e.method != http.MethodGet || e.path != "/api/admin/souls/01A" {
		t.Errorf("request = %s %s, want GET /api/admin/souls/01A", e.method, e.path)
	}
	if soul.Status != "draft" || soul.Source != "seed" {
		t.Errorf("soul = %+v, want the draft seed-owned soul", soul)
	}
}

// UpdateSoul must PATCH only the provided fields and surface tookOwnership —
// the signal that a seed-owned row just became api-owned.
func TestUpdateSoul(t *testing.T) {
	e := &adminEcho{status: http.StatusOK,
		answer: `{"soul":{"id":"01A","slug":"night-owl","source":"api"},"tookOwnership":true}`}
	c := adminClient(t, e)

	soul, took, err := c.UpdateSoul(context.Background(), "01A", map[string]any{"tagline": "new"})
	if err != nil {
		t.Fatalf("UpdateSoul: %v", err)
	}
	if e.method != http.MethodPatch || e.path != "/api/admin/souls/01A" {
		t.Errorf("request = %s %s, want PATCH /api/admin/souls/01A", e.method, e.path)
	}
	if string(e.body) != `{"tagline":"new"}` {
		t.Errorf("sent body = %s, want only the patched field", e.body)
	}
	if !took || soul.Source != "api" {
		t.Errorf("(soul, tookOwnership) = (%+v, %v), want api-owned with tookOwnership", soul, took)
	}
}

// Listing equivalents follow the same wire contract under /api/admin/listings.
func TestAdminListingMethods(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		e := &adminEcho{status: http.StatusCreated,
			answer: `{"listing":{"id":"01L","slug":"my-loop","type":"loop","source":"api"}}`}
		c := adminClient(t, e)
		listing, err := c.CreateListing(context.Background(), map[string]any{"slug": "my-loop"})
		if err != nil {
			t.Fatalf("CreateListing: %v", err)
		}
		if e.method != http.MethodPost || e.path != "/api/admin/listings" {
			t.Errorf("request = %s %s, want POST /api/admin/listings", e.method, e.path)
		}
		if listing.Slug != "my-loop" || listing.Source != "api" {
			t.Errorf("listing = %+v, want the decoded created listing", listing)
		}
	})

	t.Run("get", func(t *testing.T) {
		e := &adminEcho{status: http.StatusOK,
			answer: `{"listing":{"id":"01L","slug":"my-loop","status":"draft","source":"seed"}}`}
		c := adminClient(t, e)
		listing, err := c.AdminListing(context.Background(), "01L")
		if err != nil {
			t.Fatalf("AdminListing: %v", err)
		}
		if e.method != http.MethodGet || e.path != "/api/admin/listings/01L" {
			t.Errorf("request = %s %s, want GET /api/admin/listings/01L", e.method, e.path)
		}
		if listing.Status != "draft" {
			t.Errorf("listing = %+v, want the draft listing", listing)
		}
	})

	t.Run("update", func(t *testing.T) {
		e := &adminEcho{status: http.StatusOK,
			answer: `{"listing":{"id":"01L","slug":"my-loop","source":"api"},"tookOwnership":false}`}
		c := adminClient(t, e)
		listing, took, err := c.UpdateListing(context.Background(), "01L", map[string]any{"status": "draft"})
		if err != nil {
			t.Fatalf("UpdateListing: %v", err)
		}
		if e.method != http.MethodPatch || e.path != "/api/admin/listings/01L" {
			t.Errorf("request = %s %s, want PATCH /api/admin/listings/01L", e.method, e.path)
		}
		if string(e.body) != `{"status":"draft"}` {
			t.Errorf("sent body = %s, want only the patched field", e.body)
		}
		if took || listing.Slug != "my-loop" {
			t.Errorf("(listing, tookOwnership) = (%+v, %v), want no ownership flip", listing, took)
		}
	})
}

// Profile equivalents follow the same wire contract under /api/admin/profiles:
// create POSTs the field map and decodes the created profile (with source);
// list GETs every profile.
func TestAdminProfileMethods(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		e := &adminEcho{status: http.StatusCreated,
			answer: `{"profile":{"id":"01P","handle":"shadcn","name":"shadcn","kind":"person","source":"api"}}`}
		c := adminClient(t, e)
		profile, err := c.CreateProfile(context.Background(), map[string]any{"handle": "shadcn", "name": "shadcn"})
		if err != nil {
			t.Fatalf("CreateProfile: %v", err)
		}
		if e.method != http.MethodPost || e.path != "/api/admin/profiles" {
			t.Errorf("request = %s %s, want POST /api/admin/profiles", e.method, e.path)
		}
		var sent map[string]any
		if err := json.Unmarshal(e.body, &sent); err != nil {
			t.Fatalf("request body is not JSON: %v (%q)", err, e.body)
		}
		if sent["handle"] != "shadcn" || sent["name"] != "shadcn" {
			t.Errorf("sent body = %v, want the field map verbatim", sent)
		}
		if profile.Handle != "shadcn" || profile.Source != "api" {
			t.Errorf("profile = %+v, want the decoded created profile with source", profile)
		}
	})

	t.Run("list", func(t *testing.T) {
		e := &adminEcho{status: http.StatusOK,
			answer: `{"profiles":[{"id":"01P","handle":"anthropic","name":"Anthropic","source":"seed"},` +
				`{"id":"01Q","handle":"shadcn","name":"shadcn","source":"api"}]}`}
		c := adminClient(t, e)
		profiles, err := c.Profiles(context.Background())
		if err != nil {
			t.Fatalf("Profiles: %v", err)
		}
		if e.method != http.MethodGet || e.path != "/api/admin/profiles" {
			t.Errorf("request = %s %s, want GET /api/admin/profiles", e.method, e.path)
		}
		if len(profiles) != 2 || profiles[0].Handle != "anthropic" || profiles[1].Source != "api" {
			t.Errorf("profiles = %+v, want the two decoded profiles with source", profiles)
		}
	})
}

// Feed equivalents follow the same wire contract under /api/admin/feeds:
// list GETs every feed source; create POSTs the field map; get/update use the
// id path; sync is POST …/sync and decodes the summary.
func TestAdminFeedMethods(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		e := &adminEcho{status: http.StatusOK,
			answer: `{"feeds":[{"id":"01F","label":"Buzz","feedUrl":"https://github.com/block/buzz","kind":"github_release","defaultCategory":"Releases","defaultTags":["buzz"],"autoPublish":true,"enabled":true,"createdAt":"t","updatedAt":"t"}]}`}
		c := adminClient(t, e)
		feeds, err := c.Feeds(context.Background())
		if err != nil {
			t.Fatalf("Feeds: %v", err)
		}
		if e.method != http.MethodGet || e.path != "/api/admin/feeds" {
			t.Errorf("request = %s %s, want GET /api/admin/feeds", e.method, e.path)
		}
		if len(feeds) != 1 || feeds[0].Label != "Buzz" {
			t.Errorf("feeds = %+v, want the decoded feed list", feeds)
		}
	})

	t.Run("create", func(t *testing.T) {
		e := &adminEcho{status: http.StatusCreated,
			answer: `{"feed":{"id":"01F","label":"Buzz","feedUrl":"https://github.com/block/buzz","kind":"github_release","defaultCategory":"Releases","defaultTags":[],"autoPublish":true,"enabled":true,"createdAt":"t","updatedAt":"t"}}`}
		c := adminClient(t, e)
		feed, err := c.CreateFeed(context.Background(), map[string]any{
			"label": "Buzz", "feedUrl": "https://github.com/block/buzz",
			"kind": "github_release", "defaultCategory": "Releases", "autoPublish": true,
		})
		if err != nil {
			t.Fatalf("CreateFeed: %v", err)
		}
		if e.method != http.MethodPost || e.path != "/api/admin/feeds" {
			t.Errorf("request = %s %s, want POST /api/admin/feeds", e.method, e.path)
		}
		var sent map[string]any
		if err := json.Unmarshal(e.body, &sent); err != nil {
			t.Fatalf("request body is not JSON: %v (%q)", err, e.body)
		}
		if sent["label"] != "Buzz" || sent["autoPublish"] != true {
			t.Errorf("sent body = %v, want the field map verbatim", sent)
		}
		if feed.ID != "01F" || feed.Label != "Buzz" {
			t.Errorf("feed = %+v, want the decoded created feed", feed)
		}
	})

	t.Run("get", func(t *testing.T) {
		e := &adminEcho{status: http.StatusOK,
			answer: `{"feed":{"id":"01F","label":"Buzz","feedUrl":"u","kind":"github_release","defaultCategory":"Releases","defaultTags":[],"autoPublish":false,"enabled":true,"createdAt":"t","updatedAt":"t"}}`}
		c := adminClient(t, e)
		feed, err := c.AdminFeed(context.Background(), "01F")
		if err != nil {
			t.Fatalf("AdminFeed: %v", err)
		}
		if e.method != http.MethodGet || e.path != "/api/admin/feeds/01F" {
			t.Errorf("request = %s %s, want GET /api/admin/feeds/01F", e.method, e.path)
		}
		if feed.Label != "Buzz" {
			t.Errorf("feed = %+v, want the decoded feed", feed)
		}
	})

	t.Run("update", func(t *testing.T) {
		e := &adminEcho{status: http.StatusOK,
			answer: `{"feed":{"id":"01F","label":"Buzz","feedUrl":"u","kind":"github_release","defaultCategory":"Releases","defaultTags":[],"autoPublish":true,"enabled":false,"createdAt":"t","updatedAt":"t"}}`}
		c := adminClient(t, e)
		feed, err := c.UpdateFeed(context.Background(), "01F", map[string]any{"enabled": false})
		if err != nil {
			t.Fatalf("UpdateFeed: %v", err)
		}
		if e.method != http.MethodPatch || e.path != "/api/admin/feeds/01F" {
			t.Errorf("request = %s %s, want PATCH /api/admin/feeds/01F", e.method, e.path)
		}
		if string(e.body) != `{"enabled":false}` {
			t.Errorf("sent body = %s, want only the patched field", e.body)
		}
		if feed.Enabled {
			t.Errorf("feed.Enabled = true, want false")
		}
	})

	t.Run("sync", func(t *testing.T) {
		e := &adminEcho{status: http.StatusOK,
			answer: `{"summary":{"feedId":"01F","label":"Buzz","fetched":2,"created":2,"updated":0,"skipped":0,"itemErrors":[]}}`}
		c := adminClient(t, e)
		sum, err := c.SyncFeed(context.Background(), "01F")
		if err != nil {
			t.Fatalf("SyncFeed: %v", err)
		}
		if e.method != http.MethodPost || e.path != "/api/admin/feeds/01F/sync" {
			t.Errorf("request = %s %s, want POST /api/admin/feeds/01F/sync", e.method, e.path)
		}
		if sum.FeedID != "01F" || sum.Created != 2 {
			t.Errorf("summary = %+v, want the decoded sync summary", sum)
		}
	})
}

// A non-2xx admin response must surface as the typed *APIError carrying the
// server's envelope verbatim — the CLI prints validator messages raw.
func TestAdminErrorPassthrough(t *testing.T) {
	e := &adminEcho{status: http.StatusUnprocessableEntity,
		answer: `{"error":{"code":"invalid_input","message":"admin soul create: missing required field \"slug\""}}`}
	c := adminClient(t, e)

	_, err := c.CreateSoul(context.Background(), map[string]any{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Status != http.StatusUnprocessableEntity || apiErr.Code != "invalid_input" {
		t.Errorf("apiErr = %+v, want 422 invalid_input", apiErr)
	}
	if apiErr.Message != `admin soul create: missing required field "slug"` {
		t.Errorf("message = %q, want the validator message verbatim", apiErr.Message)
	}
}

// Admin ids are caller-supplied path segments; escaping keeps a hostile or
// malformed ref from rewriting the request path.
func TestAdminIDPathEscaped(t *testing.T) {
	e := &adminEcho{status: http.StatusNotFound,
		answer: `{"error":{"code":"not_found","message":"soul id \"a/b\" not found"}}`}
	c := adminClient(t, e)

	_, err := c.AdminSoul(context.Background(), "a/b")
	if !IsNotFound(err) {
		t.Fatalf("err = %v, want not-found", err)
	}
	if e.path != "/api/admin/souls/a%2Fb" {
		t.Errorf("path = %q, want the id path-escaped", e.path)
	}
}
