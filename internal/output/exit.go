// Package output is the single rendering, error, and exit-code authority for
// the CLI. Commands return errors and typed results through this package;
// nothing else writes to stdout/stderr or decides process exit codes.
package output

import "fmt"

// Process exit codes. These are a public contract for scripts and agents.
const (
	// ExitOK means the command succeeded.
	ExitOK = 0
	// ExitError means a generic failure.
	ExitError = 1
	// ExitCancelled means the user or the environment cancelled the run.
	ExitCancelled = 2
	// ExitNotFound means a requested resource does not exist.
	ExitNotFound = 3
	// ExitAuth means authentication is required or failed.
	ExitAuth = 4
)

// CodedError is an error carrying a process exit code, a machine-readable
// error code for the JSON envelope, and an optional remediation hint.
type CodedError struct {
	Code    int
	ErrCode string
	Message string
	Hint    string
}

// Error implements the error interface.
func (e *CodedError) Error() string { return e.Message }

// NotFoundError returns a CodedError for a missing resource (exit 3).
func NotFoundError(msg, hint string) *CodedError {
	return &CodedError{Code: ExitNotFound, ErrCode: "not_found", Message: msg, Hint: hint}
}

// AuthError returns a CodedError for missing or failed authentication (exit 4).
func AuthError(msg, hint string) *CodedError {
	return &CodedError{Code: ExitAuth, ErrCode: "auth_required", Message: msg, Hint: hint}
}

// CancelledError returns a CodedError for a cancelled run (exit 2).
func CancelledError(msg string) *CodedError {
	return &CodedError{Code: ExitCancelled, ErrCode: "cancelled", Message: msg}
}

// Errorf returns a generic CodedError (exit 1) with a formatted message.
func Errorf(format string, a ...any) *CodedError {
	return &CodedError{Code: ExitError, ErrCode: "error", Message: fmt.Sprintf(format, a...)}
}

// ErrorWithHint returns a generic CodedError (exit 1) carrying a remediation
// hint for the envelope/human output.
func ErrorWithHint(msg, hint string) *CodedError {
	return &CodedError{Code: ExitError, ErrCode: "error", Message: msg, Hint: hint}
}
