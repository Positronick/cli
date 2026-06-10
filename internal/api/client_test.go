package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/positronick/cli/internal/version"
)

// staticCreds is a fixed-value CredentialsProvider for tests.
type staticCreds struct{ c Credentials }

func (s staticCreds) Get() (Credentials, error) { return s.c, nil }

// recorder captures every request's method, path, query and headers.
type recorder struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (r *recorder) add(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, req.Clone(context.Background()))
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reqs)
}

func (r *recorder) last() *http.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reqs[len(r.reqs)-1]
}

// sleepSpy records backoff sleeps instead of actually sleeping.
type sleepSpy struct {
	mu     sync.Mutex
	sleeps []time.Duration
}

func (s *sleepSpy) sleep(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sleeps = append(s.sleeps, d)
}

func (s *sleepSpy) all() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.sleeps...)
}

func newTestClient(t *testing.T, baseURL string, creds CredentialsProvider, opts ...Option) (*Client, *sleepSpy) {
	t.Helper()
	spy := &sleepSpy{}
	c, err := New(baseURL, creds, append([]Option{WithSleep(spy.sleep)}, opts...)...)
	if err != nil {
		t.Fatalf("New(%q): %v", baseURL, err)
	}
	return c, spy
}

// Auth precedence is a security contract: an explicit API key must always win,
// and the bearer token must never be sent alongside it.
func TestAuthHeaderPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		creds    CredentialsProvider
		wantKey  string
		wantBear string
	}{
		{
			name:     "API key wins over bearer and suppresses it",
			creds:    staticCreds{Credentials{APIKey: "pk_123", Bearer: "tok_999"}},
			wantKey:  "pk_123",
			wantBear: "",
		},
		{
			name:     "bearer used when no API key",
			creds:    staticCreds{Credentials{Bearer: "tok_999"}},
			wantKey:  "",
			wantBear: "Bearer tok_999",
		},
		{
			name:     "anonymous sends neither",
			creds:    Anonymous{},
			wantKey:  "",
			wantBear: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec.add(r)
				fmt.Fprint(w, `{"souls":[]}`)
			}))
			defer srv.Close()

			c, _ := newTestClient(t, srv.URL, tt.creds)
			if _, err := c.Souls(context.Background()); err != nil {
				t.Fatalf("Souls: %v", err)
			}

			got := rec.last()
			if k := got.Header.Get("x-api-key"); k != tt.wantKey {
				t.Errorf("x-api-key = %q, want %q", k, tt.wantKey)
			}
			if b := got.Header.Get("Authorization"); b != tt.wantBear {
				t.Errorf("Authorization = %q, want %q", b, tt.wantBear)
			}
		})
	}
}

// EnvCredentials must read POSITRONICK_API_KEY at call time, not at client
// construction — agents export the key after their tooling has initialized.
func TestEnvCredentialsReadAtCallTime(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		fmt.Fprint(w, `{"souls":[]}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, EnvCredentials{})
	t.Setenv("POSITRONICK_API_KEY", "pk_from_env") // set AFTER New()

	if _, err := c.Souls(context.Background()); err != nil {
		t.Fatalf("Souls: %v", err)
	}
	if k := rec.last().Header.Get("x-api-key"); k != "pk_from_env" {
		t.Errorf("x-api-key = %q, want pk_from_env", k)
	}
}

func TestUserAgent(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		fmt.Fprint(w, `{"souls":[]}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Anonymous{})
	if _, err := c.Souls(context.Background()); err != nil {
		t.Fatalf("Souls: %v", err)
	}

	want := fmt.Sprintf("positronick-cli/%s (%s/%s)", version.Version, runtime.GOOS, runtime.GOARCH)
	if got := rec.last().Header.Get("User-Agent"); got != want {
		t.Errorf("User-Agent = %q, want %q", got, want)
	}
}

// Transient 5xx responses must be retried with growing backoff:
// 300ms*2^attempt plus up to 100ms jitter.
func TestRetryOn500Then200(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		if rec.count() <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, `{"souls":[{"slug":"sherlock"}]}`)
	}))
	defer srv.Close()

	c, spy := newTestClient(t, srv.URL, Anonymous{})
	souls, err := c.Souls(context.Background())
	if err != nil {
		t.Fatalf("Souls after retries: %v", err)
	}
	if len(souls) != 1 || souls[0].Slug != "sherlock" {
		t.Errorf("souls = %+v, want one soul with slug sherlock", souls)
	}
	if rec.count() != 3 {
		t.Errorf("requests = %d, want 3 (two retries)", rec.count())
	}

	sleeps := spy.all()
	if len(sleeps) != 2 {
		t.Fatalf("sleeps = %v, want exactly 2", sleeps)
	}
	if sleeps[0] < 300*time.Millisecond || sleeps[0] >= 400*time.Millisecond {
		t.Errorf("first backoff = %v, want [300ms,400ms)", sleeps[0])
	}
	if sleeps[1] < 600*time.Millisecond || sleeps[1] >= 700*time.Millisecond {
		t.Errorf("second backoff = %v, want [600ms,700ms)", sleeps[1])
	}
	if sleeps[1] <= sleeps[0]-100*time.Millisecond {
		t.Errorf("backoff must grow: %v then %v", sleeps[0], sleeps[1])
	}
}

// A 429 with Retry-After larger than the computed backoff must wait the
// server-mandated time instead.
func TestRetryAfterHonoredWhenLarger(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		if rec.count() == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `{"souls":[]}`)
	}))
	defer srv.Close()

	c, spy := newTestClient(t, srv.URL, Anonymous{})
	if _, err := c.Souls(context.Background()); err != nil {
		t.Fatalf("Souls: %v", err)
	}

	sleeps := spy.all()
	if len(sleeps) != 1 {
		t.Fatalf("sleeps = %v, want exactly 1", sleeps)
	}
	if sleeps[0] < 2*time.Second {
		t.Errorf("sleep = %v, want >= 2s (Retry-After: 2)", sleeps[0])
	}
}

// A Retry-After smaller than the computed backoff must NOT shrink the wait.
func TestRetryAfterSmallerIsIgnored(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		if rec.count() == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `{"souls":[]}`)
	}))
	defer srv.Close()

	c, spy := newTestClient(t, srv.URL, Anonymous{})
	if _, err := c.Souls(context.Background()); err != nil {
		t.Fatalf("Souls: %v", err)
	}
	sleeps := spy.all()
	if len(sleeps) != 1 || sleeps[0] < 300*time.Millisecond {
		t.Errorf("sleeps = %v, want one sleep >= 300ms backoff", sleeps)
	}
}

// 4xx responses (other than 429) are the caller's problem — no retries, and
// the server's error envelope becomes a typed APIError.
func TestNotFoundEnvelope(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"code":"not_found","message":"Soul not found"}}`)
	}))
	defer srv.Close()

	c, spy := newTestClient(t, srv.URL, Anonymous{})
	_, err := c.Soul(context.Background(), "nope")
	if err == nil {
		t.Fatal("Soul should return the 404 as an error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %T should be an *APIError", err)
	}
	if apiErr.Status != http.StatusNotFound {
		t.Errorf("Status = %d, want 404", apiErr.Status)
	}
	if apiErr.Code != "not_found" {
		t.Errorf("Code = %q, want not_found", apiErr.Code)
	}
	if apiErr.Message != "Soul not found" {
		t.Errorf("Message = %q, want Soul not found", apiErr.Message)
	}
	if !IsNotFound(err) {
		t.Error("IsNotFound must be true for a 404")
	}
	if IsAuthError(err) {
		t.Error("IsAuthError must be false for a 404")
	}
	if rec.count() != 1 || len(spy.all()) != 0 {
		t.Errorf("404 must not be retried: %d requests, %v sleeps", rec.count(), spy.all())
	}
}

func TestAuthErrorHelper(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":{"code":"unauthorized","message":"login required"}}`)
			}))
			defer srv.Close()

			c, _ := newTestClient(t, srv.URL, Anonymous{})
			_, err := c.Souls(context.Background())
			if !IsAuthError(err) {
				t.Errorf("IsAuthError(%v) must be true for HTTP %d", err, status)
			}
			if IsNotFound(err) {
				t.Errorf("IsNotFound(%v) must be false for HTTP %d", err, status)
			}
		})
	}
}

// A proxy or load balancer can answer with plain text or HTML; the client must
// still produce a useful APIError from the HTTP status text.
func TestNonJSONErrorBody(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `<html>boom</html>`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Anonymous{})
	_, err := c.Souls(context.Background())
	if err == nil {
		t.Fatal("persistent 500 should surface an error")
	}
	if rec.count() != 3 {
		t.Errorf("requests = %d, want 3 (max 2 retries then fail)", rec.count())
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %T should be an *APIError", err)
	}
	if apiErr.Status != http.StatusInternalServerError {
		t.Errorf("Status = %d, want 500", apiErr.Status)
	}
	if apiErr.Message != "Internal Server Error" {
		t.Errorf("Message = %q, want the HTTP status text", apiErr.Message)
	}
}

// The website 301s historical slugs to the current .md — the client must
// follow silently so old install commands keep working.
func TestRedirectFollowed(t *testing.T) {
	const body = "# SOUL\n\nYou are Sherlock.\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/souls/old-name.md", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/souls/sherlock.md", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/api/souls/sherlock.md", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Anonymous{})
	got, err := c.SoulMarkdown(context.Background(), "old-name")
	if err != nil {
		t.Fatalf("SoulMarkdown via 301: %v", err)
	}
	if got != body {
		t.Errorf("SoulMarkdown = %q, want the redirect target's body", got)
	}
}

// The .md endpoint is the install contract: the body must come back verbatim,
// byte for byte — agents pipe it straight into ~/.hermes/SOUL.md.
func TestSoulMarkdownVerbatim(t *testing.T) {
	const body = "---\nname: Sherlock\n---\n\n# SOUL.md\n\n  indented\n\ttabbed\n\ntrailing newline kept\n"
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Anonymous{})
	got, err := c.SoulMarkdown(context.Background(), "sherlock")
	if err != nil {
		t.Fatalf("SoulMarkdown: %v", err)
	}
	if got != body {
		t.Errorf("SoulMarkdown = %q, want verbatim body %q", got, body)
	}
	if p := rec.last().URL.Path; p != "/api/souls/sherlock.md" {
		t.Errorf("path = %q, want /api/souls/sherlock.md", p)
	}
}

func TestSoulDecodesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/souls/sherlock" {
			t.Errorf("path = %q, want /api/souls/sherlock", r.URL.Path)
		}
		fmt.Fprint(w, `{"soul":{"id":"01ABC","slug":"sherlock","name":"Sherlock","tags":["detective"],"downloadCount":7,"createdAt":"2026-01-01T00:00:00.000Z","content":"# SOUL\n"}}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Anonymous{})
	soul, err := c.Soul(context.Background(), "sherlock")
	if err != nil {
		t.Fatalf("Soul: %v", err)
	}
	if soul.Slug != "sherlock" || soul.Name != "Sherlock" || soul.Content != "# SOUL\n" {
		t.Errorf("soul = %+v, want decoded fields", soul)
	}
	if soul.DownloadCount != 7 || soul.CreatedAt != "2026-01-01T00:00:00.000Z" {
		t.Errorf("soul meta = %+v, want downloadCount 7 and ISO date kept as string", soul)
	}
}

// ?type= must be sent only when a type filter is set; an empty filter means
// the unfiltered listings collection.
func TestListingsQueryParam(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		fmt.Fprint(w, `{"listings":[{"slug":"github-cli","name":"GitHub CLI","type":"cli"}]}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Anonymous{})

	if _, err := c.Listings(context.Background(), ""); err != nil {
		t.Fatalf("Listings(\"\"): %v", err)
	}
	if q := rec.last().URL.RawQuery; q != "" {
		t.Errorf("empty type must send no query string, got %q", q)
	}

	ls, err := c.Listings(context.Background(), "mcp")
	if err != nil {
		t.Fatalf("Listings(\"mcp\"): %v", err)
	}
	if got := rec.last().URL.Query().Get("type"); got != "mcp" {
		t.Errorf("type param = %q, want mcp", got)
	}
	if len(ls) != 1 || ls[0].Slug != "github-cli" {
		t.Errorf("listings = %+v, want the decoded listing", ls)
	}
}

func TestListingsInvalidType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"code":"invalid_type","message":"unknown listing type"}}`)
	}))
	defer srv.Close()

	c, spy := newTestClient(t, srv.URL, Anonymous{})
	_, err := c.Listings(context.Background(), "bogus")

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %T should be an *APIError", err)
	}
	if apiErr.Code != "invalid_type" || apiErr.Status != http.StatusBadRequest {
		t.Errorf("got %+v, want invalid_type / 400", apiErr)
	}
	if len(spy.all()) != 0 {
		t.Errorf("400 must not be retried, slept %v", spy.all())
	}
}

func TestListingDecodesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/listings/github-cli" {
			t.Errorf("path = %q, want /api/listings/github-cli", r.URL.Path)
		}
		fmt.Fprint(w, `{"listing":{"slug":"github-cli","name":"GitHub CLI","type":"cli","official":true,"installCmd":"brew install gh"}}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Anonymous{})
	l, err := c.Listing(context.Background(), "github-cli")
	if err != nil {
		t.Fatalf("Listing: %v", err)
	}
	if l.Name != "GitHub CLI" || !l.Official || l.InstallCmd == nil || *l.InstallCmd != "brew install gh" {
		t.Errorf("listing = %+v, want decoded fields", l)
	}
}

// Retries are for idempotent GETs only — a future POST must never be replayed.
func TestNoRetryOnNonGet(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, spy := newTestClient(t, srv.URL, Anonymous{})
	err := c.do(context.Background(), http.MethodPost, "/api/souls", nil, nil, nil)
	if err == nil {
		t.Fatal("POST 500 should error")
	}
	if rec.count() != 1 {
		t.Errorf("requests = %d, want 1 (no retry on POST)", rec.count())
	}
	if len(spy.all()) != 0 {
		t.Errorf("no sleeps expected on POST, got %v", spy.all())
	}
}

// Connection errors (refused, reset) are retried like 5xx.
func TestConnectionErrorRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close() // closed immediately: every dial is refused

	c, spy := newTestClient(t, srv.URL, Anonymous{})
	_, err := c.Souls(context.Background())
	if err == nil {
		t.Fatal("connection refused should surface an error")
	}
	if len(spy.all()) != 2 {
		t.Errorf("sleeps = %v, want 2 (max retries)", spy.all())
	}
}

func TestNewValidation(t *testing.T) {
	t.Run("rejects a non-http scheme", func(t *testing.T) {
		if _, err := New("ftp://positronick.com", Anonymous{}); err == nil {
			t.Fatal("ftp:// must be rejected")
		}
	})

	t.Run("strips the trailing slash so paths do not double up", func(t *testing.T) {
		rec := &recorder{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec.add(r)
			fmt.Fprint(w, `{"souls":[]}`)
		}))
		defer srv.Close()

		c, _ := newTestClient(t, srv.URL+"/", Anonymous{})
		if _, err := c.Souls(context.Background()); err != nil {
			t.Fatalf("Souls: %v", err)
		}
		if p := rec.last().URL.Path; p != "/api/souls" {
			t.Errorf("path = %q, want /api/souls (no double slash)", p)
		}
	})

	t.Run("nil credentials default to anonymous", func(t *testing.T) {
		rec := &recorder{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec.add(r)
			fmt.Fprint(w, `{"souls":[]}`)
		}))
		defer srv.Close()

		c, err := New(srv.URL, nil, WithSleep(func(time.Duration) {}))
		if err != nil {
			t.Fatalf("New with nil creds: %v", err)
		}
		if _, err := c.Souls(context.Background()); err != nil {
			t.Fatalf("Souls: %v", err)
		}
		if k := rec.last().Header.Get("x-api-key"); k != "" {
			t.Errorf("nil creds must be anonymous, got x-api-key %q", k)
		}
	})
}

// A cancelled context must abort the retry loop instead of burning the
// remaining attempts.
func TestContextCancellationStopsRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cancel() // cancel while the first attempt is in flight
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Anonymous{})
	_, err := c.Souls(ctx)
	if err == nil {
		t.Fatal("cancelled context should surface an error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v should wrap context.Canceled so the CLI maps it to exit 2", err)
	}
}

// Slugs are user input on the command line; they must be path-escaped, never
// allowed to inject extra path segments.
func TestSlugPathEscaping(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"code":"not_found","message":"nope"}}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Anonymous{})
	_, _ = c.Soul(context.Background(), "a/../../etc")
	if got := rec.last().URL.EscapedPath(); got != "/api/souls/a%2F..%2F..%2Fetc" {
		t.Errorf("escaped path = %q, want the slug percent-encoded", got)
	}
}

func TestAPIErrorMessage(t *testing.T) {
	err := &APIError{Status: 404, Code: "not_found", Message: "Soul not found"}
	want := "Soul not found (HTTP 404, not_found)"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}

	bare := &APIError{Status: 500, Message: "Internal Server Error"}
	if bare.Error() != "Internal Server Error (HTTP 500)" {
		t.Errorf("Error() = %q, want status-text form", bare.Error())
	}
}

func TestIsHelpersOnForeignErrors(t *testing.T) {
	if IsNotFound(errors.New("plain")) || IsAuthError(errors.New("plain")) {
		t.Error("helpers must be false for non-API errors")
	}
	if IsNotFound(nil) || IsAuthError(nil) {
		t.Error("helpers must be false for nil")
	}
	wrapped := fmt.Errorf("fetching: %w", &APIError{Status: 404, Code: "not_found", Message: "x"})
	if !IsNotFound(wrapped) {
		t.Error("IsNotFound must see through error wrapping")
	}
}

// The 30s overall timeout is part of the client contract: a hung server must
// not hang an agent forever.
func TestDefaultTimeout(t *testing.T) {
	c, err := New("https://positronick.com", Anonymous{})
	if err != nil {
		t.Fatal(err)
	}
	if c.http.Timeout != 30*time.Second {
		t.Errorf("default timeout = %v, want 30s", c.http.Timeout)
	}
}
