package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/positronick/cli/internal/version"
)

// Credentials carries the secrets a request can authenticate with. APIKey
// always wins: when set, the bearer token is not sent at all.
type Credentials struct {
	// APIKey is sent as the x-api-key header.
	APIKey string
	// Bearer is sent as "Authorization: Bearer ..." when APIKey is empty.
	Bearer string
}

// CredentialsProvider supplies credentials per request, so tokens refreshed
// or exported mid-process are picked up. The future auth package implements
// this; the client only depends on the interface.
type CredentialsProvider interface {
	Get() (Credentials, error)
}

// Anonymous is a CredentialsProvider that never authenticates.
type Anonymous struct{}

// Get implements CredentialsProvider.
func (Anonymous) Get() (Credentials, error) { return Credentials{}, nil }

// EnvCredentials reads POSITRONICK_API_KEY from the environment at call time
// (not at construction), so a key exported after client setup still applies.
type EnvCredentials struct{}

// Get implements CredentialsProvider.
func (EnvCredentials) Get() (Credentials, error) {
	return Credentials{APIKey: os.Getenv("POSITRONICK_API_KEY")}, nil
}

const (
	// defaultTimeout bounds an entire request, redirects and body included.
	defaultTimeout = 30 * time.Second
	// maxRetries is the number of retries after the first attempt.
	maxRetries = 2
	// backoffBase is the first retry's wait; it doubles per attempt.
	backoffBase = 300 * time.Millisecond
	// jitterMax is the upper bound (exclusive) of the random jitter added to
	// each backoff so stampeding clients spread out.
	jitterMax = 100 * time.Millisecond
)

// Client is a positronick.com API client. Construct it with New; the zero
// value is not usable.
type Client struct {
	baseURL   string
	creds     CredentialsProvider
	http      *http.Client
	sleep     func(time.Duration)
	userAgent string
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient replaces the underlying *http.Client (e.g. for a custom
// transport). The caller owns the timeout configuration.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithSleep replaces the backoff sleep function — a test seam so retry tests
// assert waits instead of serving them.
func WithSleep(fn func(time.Duration)) Option { return func(c *Client) { c.sleep = fn } }

// New returns a Client for the API at baseURL. The scheme must be http or
// https; a trailing slash is stripped. A nil creds means anonymous. Redirects
// are followed (the server 301s historical soul slugs to current ones).
func New(baseURL string, creds CredentialsProvider, opts ...Option) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL %q: %w", baseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("base URL %q must use http or https", baseURL)
	}
	if creds == nil {
		creds = Anonymous{}
	}

	c := &Client{
		baseURL:   strings.TrimRight(u.String(), "/"),
		creds:     creds,
		http:      &http.Client{Timeout: defaultTimeout},
		sleep:     time.Sleep,
		userAgent: fmt.Sprintf("positronick-cli/%s (%s/%s)", version.Version, runtime.GOOS, runtime.GOARCH),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// do performs one API call: builds the request, injects auth, retries
// transient failures (idempotent GETs only), and decodes the response into
// out. out may be nil (discard), *string (raw body, for the .md endpoint), or
// any JSON-decodable value. A status >= 400 returns a *APIError decoded from
// the server's error envelope.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, out any) error {
	reqURL := c.baseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}
	retryable := method == http.MethodGet

	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%s %s: %w", method, path, err)
		}

		req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
		if err != nil {
			return fmt.Errorf("%s %s: building request: %w", method, path, err)
		}
		if err := c.authorize(req); err != nil {
			return err
		}
		req.Header.Set("User-Agent", c.userAgent)

		resp, err := c.http.Do(req)
		if err != nil {
			// Connection-level failure (refused, reset, DNS).
			if retryable && attempt < maxRetries {
				c.sleep(retryWait(attempt, nil))
				continue
			}
			return fmt.Errorf("%s %s: %w", method, path, err)
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) &&
			retryable && attempt < maxRetries {
			c.sleep(retryWait(attempt, resp))
			continue
		}
		if resp.StatusCode >= 400 {
			return decodeAPIError(resp.StatusCode, body)
		}
		if readErr != nil {
			return fmt.Errorf("%s %s: reading response: %w", method, path, readErr)
		}

		switch v := out.(type) {
		case nil:
			return nil
		case *string:
			*v = string(body)
			return nil
		default:
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("%s %s: decoding response: %w", method, path, err)
			}
			return nil
		}
	}
}

// authorize injects auth headers from the per-request credentials: an API key
// is sent as x-api-key and short-circuits; otherwise a bearer token is sent
// as Authorization; otherwise the request stays anonymous.
func (c *Client) authorize(req *http.Request) error {
	creds, err := c.creds.Get()
	if err != nil {
		return fmt.Errorf("loading credentials: %w", err)
	}
	switch {
	case creds.APIKey != "":
		req.Header.Set("x-api-key", creds.APIKey)
	case creds.Bearer != "":
		req.Header.Set("Authorization", "Bearer "+creds.Bearer)
	}
	return nil
}

// retryWait computes the wait before retry number attempt+1:
// 300ms*2^attempt plus [0,100ms) jitter, raised to the server's Retry-After
// (in seconds) when that is present and larger.
func retryWait(attempt int, resp *http.Response) time.Duration {
	wait := backoffBase<<attempt + rand.N(jitterMax)
	if resp == nil {
		return wait
	}
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil {
			if d := time.Duration(secs) * time.Second; d > wait {
				wait = d
			}
		}
	}
	return wait
}
