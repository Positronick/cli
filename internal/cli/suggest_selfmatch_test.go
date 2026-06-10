package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/output"
)

// A server that LISTS a slug while its detail endpoint 404s is an older API,
// not a user typo — the hint must not suggest the input back to the user.
func TestNotFoundNeverSuggestsTheInputItself(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/souls":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"souls": []map[string]any{{"slug": "code-reviewer", "name": "Code Reviewer"}},
			})
		case "/api/listings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"listings": []map[string]any{{"slug": "gh", "name": "GitHub CLI", "type": "cli"}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"nope"}}`))
		}
	}))
	defer srv.Close()

	client, err := api.New(srv.URL, api.Anonymous{})
	if err != nil {
		t.Fatal(err)
	}

	var coded *output.CodedError
	soulErr := soulNotFound(context.Background(), client, "code-reviewer")
	if !errors.As(soulErr, &coded) {
		t.Fatalf("want CodedError, got %T", soulErr)
	}
	if strings.Contains(coded.Hint, "did you mean") {
		t.Fatalf("self-suggestion leaked: %q", coded.Hint)
	}
	if !strings.Contains(coded.Hint, "older API") {
		t.Fatalf("want older-API explanation, got: %q", coded.Hint)
	}

	listErr := listingNotFound(context.Background(), client, "cli", "gh")
	if !errors.As(listErr, &coded) {
		t.Fatalf("want CodedError, got %T", listErr)
	}
	if strings.Contains(coded.Hint, "did you mean") {
		t.Fatalf("listing self-suggestion leaked: %q", coded.Hint)
	}
}
