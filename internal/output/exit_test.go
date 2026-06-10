package output

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// The exit code and JSON envelope are a public contract: agents and scripts
// branch on them. These tests pin the error→code mapping and the envelope
// shape so a change here is a deliberate contract change, not an accident.
func TestRenderErrorClassification(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantCode    int
		wantErrCode string
		wantMessage string
		wantHint    string
	}{
		{
			name:        "not found error",
			err:         NotFoundError("soul \"sherlock\" not found", "run `positronick soul list`"),
			wantCode:    ExitNotFound,
			wantErrCode: "not_found",
			wantMessage: "soul \"sherlock\" not found",
			wantHint:    "run `positronick soul list`",
		},
		{
			name:        "auth error",
			err:         AuthError("not logged in", "run `positronick login`"),
			wantCode:    ExitAuth,
			wantErrCode: "auth_required",
			wantMessage: "not logged in",
			wantHint:    "run `positronick login`",
		},
		{
			name:        "cancelled error",
			err:         CancelledError("aborted by user"),
			wantCode:    ExitCancelled,
			wantErrCode: "cancelled",
			wantMessage: "aborted by user",
		},
		{
			name:        "errorf",
			err:         Errorf("bad input: %s", "x"),
			wantCode:    ExitError,
			wantErrCode: "error",
			wantMessage: "bad input: x",
		},
		{
			name:        "raw context.Canceled maps to cancelled",
			err:         context.Canceled,
			wantCode:    ExitCancelled,
			wantErrCode: "cancelled",
			wantMessage: "context canceled",
		},
		{
			name:        "wrapped context.Canceled maps to cancelled",
			err:         fmt.Errorf("fetching souls: %w", context.Canceled),
			wantCode:    ExitCancelled,
			wantErrCode: "cancelled",
			wantMessage: "fetching souls: context canceled",
		},
		{
			name:        "unknown error defaults to generic error",
			err:         errors.New("boom"),
			wantCode:    ExitError,
			wantErrCode: "error",
			wantMessage: "boom",
		},
		{
			name:        "wrapped CodedError keeps its code",
			err:         fmt.Errorf("install: %w", NotFoundError("missing", "")),
			wantCode:    ExitNotFound,
			wantErrCode: "not_found",
			wantMessage: "missing",
		},
		{
			name:        "CodedError with unset ErrCode defaults from exit code",
			err:         &CodedError{Code: ExitAuth, Message: "token expired"},
			wantCode:    ExitAuth,
			wantErrCode: "auth_required",
			wantMessage: "token expired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/json", func(t *testing.T) {
			var buf bytes.Buffer
			got := RenderError(&buf, tt.err, true)
			if got != tt.wantCode {
				t.Errorf("exit code = %d, want %d", got, tt.wantCode)
			}

			line := buf.String()
			if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
				t.Errorf("JSON envelope must be exactly one line, got %q", line)
			}

			var envelope map[string]map[string]string
			if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
				t.Fatalf("envelope is not valid JSON: %v (%q)", err, line)
			}
			body, ok := envelope["error"]
			if !ok {
				t.Fatalf("envelope missing top-level \"error\" key: %q", line)
			}
			if body["code"] != tt.wantErrCode {
				t.Errorf("error.code = %q, want %q", body["code"], tt.wantErrCode)
			}
			if body["message"] != tt.wantMessage {
				t.Errorf("error.message = %q, want %q", body["message"], tt.wantMessage)
			}
			if tt.wantHint == "" {
				if _, present := body["hint"]; present {
					t.Errorf("empty hint must be omitted from JSON, got %q", line)
				}
			} else if body["hint"] != tt.wantHint {
				t.Errorf("error.hint = %q, want %q", body["hint"], tt.wantHint)
			}
		})

		t.Run(tt.name+"/human", func(t *testing.T) {
			var buf bytes.Buffer
			got := RenderError(&buf, tt.err, false)
			if got != tt.wantCode {
				t.Errorf("exit code = %d, want %d", got, tt.wantCode)
			}

			want := "error: " + tt.wantMessage + "\n"
			if tt.wantHint != "" {
				want += "hint: " + tt.wantHint + "\n"
			}
			if buf.String() != want {
				t.Errorf("human output = %q, want %q", buf.String(), want)
			}
		})
	}
}

func TestCodedErrorImplementsError(t *testing.T) {
	var err error = NotFoundError("nope", "")
	if err.Error() != "nope" {
		t.Errorf("Error() = %q, want %q", err.Error(), "nope")
	}
}
