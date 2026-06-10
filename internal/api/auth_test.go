package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// /api/me is how the CLI verifies credentials: the decoded identity and the
// server-computed isAdmin bit drive `auth status` and the credential cache.
func TestMeDecodes(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		fmt.Fprint(w, `{"user":{"id":"usr_1","name":"Ada Lovelace","email":"ada@example.com",`+
			`"image":"https://example.com/ada.png","githubLogin":"ada"},"isAdmin":true}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, staticCreds{Credentials{Bearer: "tok_1"}})
	me, err := c.Me(context.Background())
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if got := rec.last(); got.Method != http.MethodGet || got.URL.Path != "/api/me" {
		t.Errorf("request = %s %s, want GET /api/me", got.Method, got.URL.Path)
	}
	if me.User.ID != "usr_1" || me.User.Name != "Ada Lovelace" || me.User.Email != "ada@example.com" {
		t.Errorf("user = %+v, want decoded identity", me.User)
	}
	if me.User.Image == nil || *me.User.Image != "https://example.com/ada.png" {
		t.Errorf("image = %v, want the avatar URL", me.User.Image)
	}
	if me.User.GithubLogin == nil || *me.User.GithubLogin != "ada" {
		t.Errorf("githubLogin = %v, want ada", me.User.GithubLogin)
	}
	if !me.IsAdmin {
		t.Error("isAdmin must decode true")
	}
}

// Nullable identity fields (image, githubLogin — e.g. a Google-only account)
// must round-trip as null, never as "".
func TestMeNullableFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"user":{"id":"usr_2","name":"G User","email":"g@example.com",`+
			`"image":null,"githubLogin":null},"isAdmin":false}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, staticCreds{Credentials{Bearer: "tok_1"}})
	me, err := c.Me(context.Background())
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if me.User.Image != nil || me.User.GithubLogin != nil {
		t.Errorf("nullable fields = %v/%v, want nil/nil", me.User.Image, me.User.GithubLogin)
	}
}

// A 401 from /api/me must surface as a typed auth error so callers can map it
// to exit 4 (or, for `auth status`, to authenticated:false).
func TestMeUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"code":"unauthorized","message":"login required"}}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Anonymous{})
	_, err := c.Me(context.Background())
	if !IsAuthError(err) {
		t.Errorf("IsAuthError(%v) must be true for a 401", err)
	}
}

// api-key/create is a JSON POST — never form-encoded (the server 415s form
// posts) — and expiresIn is omitted entirely when unset so the server applies
// its own default expiry.
func TestCreateAPIKeySendsJSONBody(t *testing.T) {
	var gotCT string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		fmt.Fprint(w, `{"id":"key_1","name":"ci","start":"posi_abc","prefix":"posi_",`+
			`"key":"posi_abcdef123456","expiresAt":"2026-09-08T10:00:00.000Z"}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, staticCreds{Credentials{Bearer: "tok_1"}})

	key, err := c.CreateAPIKey(context.Background(), "ci", 0)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
	if gotBody["name"] != "ci" {
		t.Errorf("body name = %v, want ci", gotBody["name"])
	}
	if _, present := gotBody["expiresIn"]; present {
		t.Errorf("expiresIn must be omitted when 0, body = %v", gotBody)
	}
	if key.Key != "posi_abcdef123456" || key.ID != "key_1" {
		t.Errorf("key = %+v, want decoded key object", key)
	}
	if key.ExpiresAt == nil || *key.ExpiresAt != "2026-09-08T10:00:00.000Z" {
		t.Errorf("expiresAt = %v, want the ISO string kept verbatim", key.ExpiresAt)
	}

	if _, err := c.CreateAPIKey(context.Background(), "ci", 86400); err != nil {
		t.Fatalf("CreateAPIKey with expiry: %v", err)
	}
	if gotBody["expiresIn"] != float64(86400) {
		t.Errorf("body expiresIn = %v, want 86400 seconds", gotBody["expiresIn"])
	}
}
