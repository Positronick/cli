package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Posts sends ?kind= only when set, and decodes the {posts} envelope into
// PostCards.
func TestPostsKindParam(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		if r.URL.Path != "/api/blog" {
			t.Errorf("path = %q, want /api/blog", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"posts":[{"slug":"intro","kind":"article"}]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Anonymous{})

	// Empty kind sends no query string at all.
	if _, err := c.Posts(context.Background(), ""); err != nil {
		t.Fatalf("Posts(all): %v", err)
	}
	if q := rec.last().URL.RawQuery; q != "" {
		t.Errorf("empty kind must send no query string, got %q", q)
	}

	posts, err := c.Posts(context.Background(), "article")
	if err != nil {
		t.Fatalf("Posts(article): %v", err)
	}
	if got := rec.last().URL.Query().Get("kind"); got != "article" {
		t.Errorf("kind param = %q, want article", got)
	}
	if len(posts) != 1 || posts[0].Slug != "intro" {
		t.Errorf("posts = %+v, want the decoded card", posts)
	}
}

// Post decodes the {post} envelope (body included) and surfaces a 404 as an
// IsNotFound *APIError, never a panic.
func TestPostDetailAndNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/blog/intro", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"post":{"slug":"intro","kind":"article","content":"# Intro\n"}}`))
	})
	mux.HandleFunc("/api/blog/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"post \"missing\" not found"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Anonymous{})

	post, err := c.Post(context.Background(), "intro")
	if err != nil {
		t.Fatalf("Post(intro): %v", err)
	}
	if post.Slug != "intro" || post.Content != "# Intro\n" {
		t.Errorf("post = %+v, want the decoded post with its body", post)
	}

	if _, err := c.Post(context.Background(), "missing"); !IsNotFound(err) {
		t.Errorf("Post(missing) err = %v, want IsNotFound", err)
	}
}

// The .md endpoint is the raw-markdown contract: the body comes back verbatim,
// byte for byte, from /api/blog/{slug}.md, and a historical slug's 301 is
// followed silently.
func TestPostMarkdownVerbatimAndRedirect(t *testing.T) {
	const body = "---\ntitle: Intro\n---\n\n# Intro\n\n  indented\n\ttabbed\n\ntrailing newline kept\n"
	rec := &recorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/blog/old-name.md", func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		http.Redirect(w, r, "/api/blog/intro.md", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/api/blog/intro.md", func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Anonymous{})

	got, err := c.PostMarkdown(context.Background(), "intro")
	if err != nil {
		t.Fatalf("PostMarkdown: %v", err)
	}
	if got != body {
		t.Errorf("PostMarkdown = %q, want verbatim body %q", got, body)
	}
	if p := rec.last().URL.Path; p != "/api/blog/intro.md" {
		t.Errorf("path = %q, want /api/blog/intro.md", p)
	}

	got, err = c.PostMarkdown(context.Background(), "old-name")
	if err != nil {
		t.Fatalf("PostMarkdown via 301: %v", err)
	}
	if got != body {
		t.Errorf("PostMarkdown via 301 = %q, want the redirect target's body", got)
	}
}
