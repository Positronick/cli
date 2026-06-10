package output

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// defaultErrCode maps an exit code to its default machine-readable error code.
func defaultErrCode(exitCode int) string {
	switch exitCode {
	case ExitCancelled:
		return "cancelled"
	case ExitNotFound:
		return "not_found"
	case ExitAuth:
		return "auth_required"
	default:
		return "error"
	}
}

// RenderError writes err to w — as a one-line JSON envelope
// {"error":{"code":"...","message":"...","hint":"..."}} when jsonMode is
// true, otherwise as human-readable "error: ..." / "hint: ..." lines — and
// returns the process exit code for the error. It must be called exactly
// once per failed invocation.
func RenderError(w io.Writer, err error, jsonMode bool) int {
	exitCode := ExitError
	errCode := "error"
	message := err.Error()
	hint := ""

	var coded *CodedError
	switch {
	case errors.As(err, &coded):
		exitCode = coded.Code
		errCode = coded.ErrCode
		if errCode == "" {
			errCode = defaultErrCode(exitCode)
		}
		message = coded.Message
		hint = coded.Hint
	case errors.Is(err, context.Canceled):
		exitCode = ExitCancelled
		errCode = "cancelled"
	}

	if jsonMode {
		b, mErr := json.Marshal(errorEnvelope{Error: errorBody{Code: errCode, Message: message, Hint: hint}})
		if mErr != nil {
			fmt.Fprintf(w, `{"error":{"code":"error","message":%q}}`+"\n", mErr.Error())
			return exitCode
		}
		fmt.Fprintln(w, string(b))
		return exitCode
	}

	fmt.Fprintf(w, "error: %s\n", message)
	if hint != "" {
		fmt.Fprintf(w, "hint: %s\n", hint)
	}
	return exitCode
}
