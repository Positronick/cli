package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// APIError is a non-2xx response from the API, decoded from the server's
// error envelope {"error":{"code":"...","message":"..."}} when present, or
// synthesized from the HTTP status text when the body is not the envelope
// (e.g. a proxy answering with HTML).
type APIError struct {
	// Status is the HTTP status code.
	Status int
	// Code is the machine-readable code from the envelope ("" when absent).
	Code string
	// Message is human-readable; never empty.
	Message string
	// Body is the raw response body, retained so a caller can read a
	// non-envelope payload the generic decoder ignores — e.g. the feed-sync
	// 502 answers with {"summary":{...}} (carrying the failure reason) rather
	// than the error envelope. Nil when the response had no body.
	Body []byte
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s (HTTP %d, %s)", e.Message, e.Status, e.Code)
	}
	return fmt.Sprintf("%s (HTTP %d)", e.Message, e.Status)
}

// IsNotFound reports whether err is an APIError for a missing resource (404).
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// IsAuthError reports whether err is an APIError for missing or insufficient
// authentication (401 or 403).
func IsAuthError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) &&
		(apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden)
}

// errorEnvelope mirrors the server's error body shape.
type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// decodeAPIError builds an APIError from a response body. A body that is not
// the JSON envelope (or an envelope with an empty message) falls back to the
// HTTP status text so the error is always useful.
func decodeAPIError(status int, body []byte) *APIError {
	apiErr := &APIError{Status: status, Message: http.StatusText(status), Body: body}
	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err == nil {
		apiErr.Code = env.Error.Code
		if env.Error.Message != "" {
			apiErr.Message = env.Error.Message
		}
	}
	return apiErr
}
