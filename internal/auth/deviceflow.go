// Package auth owns authentication for the CLI: the OAuth-style device flow
// against positronick.com, the on-disk credential cache, and the
// api.CredentialsProvider every command authenticates through.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/positronick/cli/internal/output"
)

// clientID identifies this CLI to the device-authorization endpoints.
const clientID = "positronick-cli"

// slowDownStep is how much each slow_down response grows the poll interval,
// per RFC 8628 §3.5 (the server enforces this pacing).
const slowDownStep = 5 * time.Second

// The device endpoints are unauthenticated and polling owns its own loop, so
// they use a plain client rather than api.Client's retry policy.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// sleep and now are test seams: tests record sleeps and advance a fake clock
// so deadline behavior is verifiable without real waiting. sleep is
// context-aware so Ctrl-C interrupts a poll wait immediately.
var (
	sleep = func(ctx context.Context, d time.Duration) error {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
	now = time.Now
)

// DeviceAuth is a started device authorization: the code the user types, the
// URLs to type it at, and the polling parameters.
type DeviceAuth struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresIn               time.Duration
	Interval                time.Duration
}

// Token is a granted access token. It is a server session token with a
// sliding ~7-day lifetime and no refresh token: when it later expires (401),
// the fix is to log in again.
type Token struct {
	AccessToken string
	TokenType   string
	ExpiresIn   time.Duration
}

// deviceCodeResponse mirrors the server's snake_case device/code response;
// the numbers are in seconds.
type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// tokenResponse mirrors the server's snake_case device/token success response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// oauthError is the flat OAuth error shape both device endpoints answer 400
// with — not the API's {"error":{...}} envelope.
type oauthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// Start begins a device authorization: POST {base}/api/auth/device/code.
func Start(ctx context.Context, baseURL string) (*DeviceAuth, error) {
	status, body, err := postJSON(ctx, baseURL+"/api/auth/device/code",
		map[string]string{"client_id": clientID})
	if err != nil {
		return nil, fmt.Errorf("starting device authorization: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("starting device authorization: %s", oauthFailure(status, body))
	}

	var resp deviceCodeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("starting device authorization: decoding response: %w", err)
	}
	return &DeviceAuth{
		DeviceCode:              resp.DeviceCode,
		UserCode:                resp.UserCode,
		VerificationURI:         resp.VerificationURI,
		VerificationURIComplete: resp.VerificationURIComplete,
		ExpiresIn:               time.Duration(resp.ExpiresIn) * time.Second,
		Interval:                time.Duration(resp.Interval) * time.Second,
	}, nil
}

// Poll polls POST {base}/api/auth/device/token every interval until the user
// approves (returning the token), denies (exit 2), or the code expires (exit
// 4) — with a hard local deadline at expiresIn even if the server keeps
// answering pending. A slow_down response grows the interval by 5s. onWait,
// if non-nil, is called before each wait so callers can show progress.
func Poll(ctx context.Context, baseURL, deviceCode string, interval, expiresIn time.Duration,
	onWait func()) (*Token, error) {
	deadline := now().Add(expiresIn)
	for {
		if onWait != nil {
			onWait()
		}
		if err := sleep(ctx, interval); err != nil {
			return nil, err
		}
		if now().After(deadline) {
			return nil, output.AuthError(
				"device authorization expired before approval", "run positronick login again")
		}

		status, body, err := postJSON(ctx, baseURL+"/api/auth/device/token", map[string]string{
			"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
			"device_code": deviceCode,
			"client_id":   clientID,
		})
		if err != nil {
			return nil, fmt.Errorf("polling for approval: %w", err)
		}

		if status == http.StatusOK {
			var resp tokenResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				return nil, fmt.Errorf("polling for approval: decoding token: %w", err)
			}
			return &Token{
				AccessToken: resp.AccessToken,
				TokenType:   resp.TokenType,
				ExpiresIn:   time.Duration(resp.ExpiresIn) * time.Second,
			}, nil
		}

		var oerr oauthError
		_ = json.Unmarshal(body, &oerr)
		switch oerr.Error {
		case "authorization_pending":
			// Keep polling.
		case "slow_down":
			interval += slowDownStep
		case "access_denied":
			return nil, output.CancelledError("authorization denied")
		case "expired_token":
			return nil, output.AuthError("device code expired", "run positronick login again")
		default:
			return nil, fmt.Errorf("polling for approval: %s", oauthFailure(status, body))
		}
	}
}

// postJSON sends one JSON POST and returns the status and raw body; the
// callers map status/body to flow outcomes.
func postJSON(ctx context.Context, url string, in any) (int, []byte, error) {
	payload, err := json.Marshal(in)
	if err != nil {
		return 0, nil, fmt.Errorf("encoding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("reading response: %w", err)
	}
	return resp.StatusCode, body, nil
}

// oauthFailure renders an unexpected device-endpoint failure: the OAuth error
// code and description when the body carries them, the HTTP status otherwise.
func oauthFailure(status int, body []byte) string {
	var oerr oauthError
	if err := json.Unmarshal(body, &oerr); err == nil && oerr.Error != "" {
		if oerr.ErrorDescription != "" {
			return fmt.Sprintf("%s: %s", oerr.Error, oerr.ErrorDescription)
		}
		return oerr.Error
	}
	return fmt.Sprintf("HTTP %d", status)
}
