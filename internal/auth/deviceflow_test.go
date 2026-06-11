package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/positronick/cli/internal/output"
)

// fakeClock replaces the package's sleep and now seams: sleeps are recorded
// and advance the fake time instead of blocking, so deadline behavior is
// testable without waiting.
type fakeClock struct {
	mu     sync.Mutex
	t      time.Time
	sleeps []time.Duration
}

func installClock(t *testing.T) *fakeClock {
	t.Helper()
	c := &fakeClock{t: time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)}
	oldSleep, oldNow := sleep, now
	sleep = func(ctx context.Context, d time.Duration) error {
		c.mu.Lock()
		c.sleeps = append(c.sleeps, d)
		c.t = c.t.Add(d)
		c.mu.Unlock()
		return ctx.Err()
	}
	now = func() time.Time {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.t
	}
	t.Cleanup(func() { sleep, now = oldSleep, oldNow })
	return c
}

func (c *fakeClock) all() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.sleeps...)
}

// deviceCodeHandler asserts the Start request shape — JSON body, never form
// encoding (the server 415s form posts) — and answers the verified snake_case
// response with second-valued numbers.
func TestStart(t *testing.T) {
	var gotMethod, gotPath, gotCT string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotCT = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		fmt.Fprint(w, `{"device_code":"dev-1","user_code":"TJSPLLAV",`+
			`"verification_uri":"https://positronick.com/device",`+
			`"verification_uri_complete":"https://positronick.com/device?user_code=TJSPLLAV",`+
			`"expires_in":1800,"interval":5}`)
	}))
	defer srv.Close()

	da, err := Start(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/auth/device/code" {
		t.Errorf("request = %s %s, want POST /api/auth/device/code", gotMethod, gotPath)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json (server 415s form posts)", gotCT)
	}
	if gotBody["client_id"] != "positronick-cli" {
		t.Errorf("client_id = %v, want positronick-cli", gotBody["client_id"])
	}
	if da.DeviceCode != "dev-1" || da.UserCode != "TJSPLLAV" {
		t.Errorf("DeviceAuth = %+v, want decoded codes", da)
	}
	if da.VerificationURI != "https://positronick.com/device" ||
		da.VerificationURIComplete != "https://positronick.com/device?user_code=TJSPLLAV" {
		t.Errorf("verification URIs = %q / %q, want decoded", da.VerificationURI, da.VerificationURIComplete)
	}
	if da.ExpiresIn != 30*time.Minute || da.Interval != 5*time.Second {
		t.Errorf("expires/interval = %v/%v, want 30m/5s (seconds decoded to durations)", da.ExpiresIn, da.Interval)
	}
}

func TestStartInvalidClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_client","error_description":"unknown client"}`)
	}))
	defer srv.Close()

	_, err := Start(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("invalid_client must surface an error")
	}
	for _, want := range []string{"invalid_client", "unknown client"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// A proxy answering with HTML must still produce a useful error, never a
// silent zero DeviceAuth.
func TestStartNonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, `<html>boom</html>`)
	}))
	defer srv.Close()

	_, err := Start(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("error = %v, want one naming HTTP 502", err)
	}
}

// scriptedTokens serves a queue of token-endpoint responses and records each
// request body.
type scriptedTokens struct {
	mu        sync.Mutex
	responses []string // raw JSON bodies; non-200 inferred from {"error":...} flat shape
	bodies    []map[string]any
}

func (s *scriptedTokens) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/device/token" {
			t.Errorf("path = %q, want /api/auth/device/token", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)

		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		if len(s.responses) == 0 {
			s.mu.Unlock()
			t.Error("token endpoint polled more times than scripted")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		resp := s.responses[0]
		s.responses = s.responses[1:]
		s.mu.Unlock()

		if after, ok := strings.CutPrefix(resp, "429:"); ok {
			// Better Auth's rate-limit shape: 429 + X-Retry-After, body without "error".
			if after != "0" {
				w.Header().Set("X-Retry-After", after)
			}
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"message":"Too many requests. Please try again later."}`)
			return
		}
		if strings.Contains(resp, `"error"`) {
			w.WriteHeader(http.StatusBadRequest)
		}
		fmt.Fprint(w, resp)
	}
}

// The happy path: pending → slow_down → success. slow_down must grow the wait
// by exactly +5s (server-enforced pacing; verified live), and every poll must
// carry the device_code grant.
func TestPollPendingSlowDownSuccess(t *testing.T) {
	clk := installClock(t)
	script := &scriptedTokens{responses: []string{
		`{"error":"authorization_pending"}`,
		`{"error":"slow_down"}`,
		`{"access_token":"tok-123","token_type":"Bearer","expires_in":604799,"scope":""}`,
	}}
	srv := httptest.NewServer(script.handler(t))
	defer srv.Close()

	waits := 0
	tok, err := Poll(context.Background(), srv.URL, "dev-1", 5*time.Second, 30*time.Minute,
		func() { waits++ })
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if tok.AccessToken != "tok-123" || tok.TokenType != "Bearer" {
		t.Errorf("token = %+v, want decoded access token", tok)
	}
	if tok.ExpiresIn != 604799*time.Second {
		t.Errorf("ExpiresIn = %v, want 604799s decoded to a duration", tok.ExpiresIn)
	}

	wantSleeps := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second}
	gotSleeps := clk.all()
	if len(gotSleeps) != len(wantSleeps) {
		t.Fatalf("sleeps = %v, want %v", gotSleeps, wantSleeps)
	}
	for i, want := range wantSleeps {
		if gotSleeps[i] != want {
			t.Errorf("sleep[%d] = %v, want %v (slow_down adds +5s)", i, gotSleeps[i], want)
		}
	}
	if waits != 3 {
		t.Errorf("onWait calls = %d, want 3 (one per poll wait)", waits)
	}

	for i, body := range script.bodies {
		if body["grant_type"] != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Errorf("poll %d grant_type = %v, want the device_code grant", i, body["grant_type"])
		}
		if body["device_code"] != "dev-1" || body["client_id"] != "positronick-cli" {
			t.Errorf("poll %d body = %v, want device_code dev-1 and client_id positronick-cli", i, body)
		}
	}
}

// A denial is a cancellation, not a failure: exit 2 with a clear message.
func TestPollAccessDenied(t *testing.T) {
	installClock(t)
	script := &scriptedTokens{responses: []string{`{"error":"access_denied"}`}}
	srv := httptest.NewServer(script.handler(t))
	defer srv.Close()

	_, err := Poll(context.Background(), srv.URL, "dev-1", 5*time.Second, 30*time.Minute, nil)
	var coded *output.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error %T should be a *output.CodedError", err)
	}
	if coded.Code != output.ExitCancelled {
		t.Errorf("exit code = %d, want %d (cancelled)", coded.Code, output.ExitCancelled)
	}
	if coded.Message != "authorization denied" {
		t.Errorf("message = %q, want %q", coded.Message, "authorization denied")
	}
}

// A server-side expiry is exit 4 with the re-login hint.
func TestPollExpiredToken(t *testing.T) {
	installClock(t)
	script := &scriptedTokens{responses: []string{`{"error":"expired_token"}`}}
	srv := httptest.NewServer(script.handler(t))
	defer srv.Close()

	_, err := Poll(context.Background(), srv.URL, "dev-1", 5*time.Second, 30*time.Minute, nil)
	var coded *output.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error %T should be a *output.CodedError", err)
	}
	if coded.Code != output.ExitAuth {
		t.Errorf("exit code = %d, want %d (auth)", coded.Code, output.ExitAuth)
	}
	if coded.Hint != "run positronick login again" {
		t.Errorf("hint = %q, want the re-login hint", coded.Hint)
	}
}

// The client enforces its own hard deadline at expires_in: a server that
// keeps answering pending must not be polled forever.
func TestPollLocalDeadline(t *testing.T) {
	clk := installClock(t)
	script := &scriptedTokens{responses: []string{
		`{"error":"authorization_pending"}`,
		`{"error":"authorization_pending"}`,
	}}
	srv := httptest.NewServer(script.handler(t))
	defer srv.Close()

	_, err := Poll(context.Background(), srv.URL, "dev-1", 5*time.Second, 12*time.Second, nil)
	var coded *output.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error %T should be a *output.CodedError", err)
	}
	if coded.Code != output.ExitAuth {
		t.Errorf("exit code = %d, want %d (auth)", coded.Code, output.ExitAuth)
	}
	if coded.Hint != "run positronick login again" {
		t.Errorf("hint = %q, want the re-login hint", coded.Hint)
	}
	// Two polls at t=5s and t=10s fit inside the 12s budget; the third wait
	// crosses the deadline, so the endpoint is hit exactly twice.
	if got := len(clk.all()); got != 3 {
		t.Errorf("sleeps = %d, want 3 (the third crosses the deadline)", got)
	}
	if got := len(script.bodies); got != 2 {
		t.Errorf("polls = %d, want exactly 2", got)
	}
}

// Ctrl-C during the wait must abort promptly with the context error so the
// CLI maps it to exit 2.
func TestPollContextCancelled(t *testing.T) {
	installClock(t)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("a cancelled context must not reach the network")
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Poll(ctx, srv.URL, "dev-1", 5*time.Second, 30*time.Minute, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

// An unrecognized OAuth error must fail loudly with the server's code.
func TestPollUnknownError(t *testing.T) {
	installClock(t)
	script := &scriptedTokens{responses: []string{`{"error":"invalid_grant"}`}}
	srv := httptest.NewServer(script.handler(t))
	defer srv.Close()

	_, err := Poll(context.Background(), srv.URL, "dev-1", 5*time.Second, 30*time.Minute, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("error = %v, want one naming invalid_grant", err)
	}
}

// A rate-limited poll (HTTP 429 — a proxy, CDN, or the server's IP limiter)
// must back off and keep polling, honoring X-Retry-After; it is never fatal.
// Found live: production 429'd a 5s-cadence poll and login died mid-flow.
func TestPollRetriesOn429HonoringRetryAfter(t *testing.T) {
	clk := installClock(t)
	script := &scriptedTokens{responses: []string{
		"429:17",
		`{"access_token":"tok-9","token_type":"Bearer","expires_in":604799,"scope":""}`,
	}}
	srv := httptest.NewServer(script.handler(t))
	defer srv.Close()

	tok, err := Poll(context.Background(), srv.URL, "dev-1", 5*time.Second, 30*time.Minute, nil)
	if err != nil {
		t.Fatalf("Poll after 429: %v", err)
	}
	if tok.AccessToken != "tok-9" {
		t.Errorf("AccessToken = %q, want tok-9", tok.AccessToken)
	}
	// Base-interval wait first, then the 429's X-Retry-After (17s > 5s) wins once.
	wantSleeps := []time.Duration{5 * time.Second, 17 * time.Second}
	gotSleeps := clk.all()
	if len(gotSleeps) != len(wantSleeps) {
		t.Fatalf("sleeps = %v, want %v", gotSleeps, wantSleeps)
	}
	for i, want := range wantSleeps {
		if gotSleeps[i] != want {
			t.Errorf("sleep[%d] = %v, want %v", i, gotSleeps[i], want)
		}
	}
}

// A 429 without a usable Retry-After still backs off (the slow_down step) and
// the flow recovers; the wait returns to the base interval afterwards.
func TestPollRetriesOn429WithoutHeader(t *testing.T) {
	clk := installClock(t)
	script := &scriptedTokens{responses: []string{
		"429:0",
		`{"error":"authorization_pending"}`,
		`{"access_token":"tok-10","token_type":"Bearer","expires_in":604799,"scope":""}`,
	}}
	srv := httptest.NewServer(script.handler(t))
	defer srv.Close()

	if _, err := Poll(context.Background(), srv.URL, "dev-1", 5*time.Second, 30*time.Minute, nil); err != nil {
		t.Fatalf("Poll after headerless 429: %v", err)
	}
	// 5s base, then 10s backoff for the 429, then back to the 5s base.
	wantSleeps := []time.Duration{5 * time.Second, 10 * time.Second, 5 * time.Second}
	gotSleeps := clk.all()
	if len(gotSleeps) != len(wantSleeps) {
		t.Fatalf("sleeps = %v, want %v", gotSleeps, wantSleeps)
	}
	for i, want := range wantSleeps {
		if gotSleeps[i] != want {
			t.Errorf("sleep[%d] = %v, want %v", i, gotSleeps[i], want)
		}
	}
}
