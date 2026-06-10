package cli

import (
	"bytes"
	"context"
	"net"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/mockapi"
	"github.com/positronick/cli/internal/output"
)

// executeAgainst runs the root command against the mock API and, on failure,
// renders the error exactly the way Execute() does — so tests assert the real
// exit code and the real envelope agents would see.
func executeAgainst(t *testing.T, baseURL string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	if baseURL != "" {
		args = append(args, "--base-url", baseURL)
	}
	root := NewRootCmd()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	if err := root.ExecuteContext(context.Background()); err != nil {
		exitCode = output.RenderError(&errBuf, err, wasJSONRequested(root, args))
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func newMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(mockapi.Handler())
	t.Cleanup(srv.Close)
	return srv
}

// An empty search query is an error with a pointer to `list` — agents should
// never silently get the full set when they asked to search for nothing.
func TestSearchRequiresQuery(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"soul search no args", []string{"soul", "search", "--json"}},
		{"soul search blank arg", []string{"soul", "search", "   ", "--json"}},
		{"mcp search no args", []string{"mcp", "search", "--json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			noun := tt.args[0]
			stdout, stderr, code := executeAgainst(t, "", tt.args...)
			if code != output.ExitError {
				t.Errorf("exit code = %d, want %d", code, output.ExitError)
			}
			if stdout != "" {
				t.Errorf("stdout must stay clean on error, got %q", stdout)
			}
			if !strings.Contains(stderr, noun+" search requires a query") {
				t.Errorf("stderr = %q, want the requires-a-query message", stderr)
			}
			if !strings.Contains(stderr, "positronick "+noun+" list") {
				t.Errorf("stderr = %q, want a hint pointing at %s list", stderr, noun)
			}
		})
	}
}

// Flag validation fails before any network call, with the valid values named.
func TestInvalidFlagValues(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantMessage string
	}{
		{
			"unknown sort key",
			[]string{"soul", "search", "o", "--sort", "bogus"},
			`invalid --sort "bogus" (valid: relevance, name, downloads, newest)`,
		},
		{
			"unknown sort key on list",
			[]string{"cli", "list", "--sort", "best"},
			`invalid --sort "best" (valid: relevance, name, downloads, newest)`,
		},
		{
			"zero limit",
			[]string{"soul", "list", "--limit", "0"},
			"--limit must be at least 1, got 0",
		},
		{
			"negative limit",
			[]string{"mcp", "search", "graf", "--limit", "-5"},
			"--limit must be at least 1, got -5",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No server: validation must reject the flags before any request.
			_, stderr, code := executeAgainst(t, "http://127.0.0.1:1", tt.args...)
			if code != output.ExitError {
				t.Errorf("exit code = %d, want %d", code, output.ExitError)
			}
			if !strings.Contains(stderr, tt.wantMessage) {
				t.Errorf("stderr = %q, want %q", stderr, tt.wantMessage)
			}
		})
	}
}

// `--framework` exists only for souls: listings have no framework facet.
func TestFrameworkFlagIsSoulOnly(t *testing.T) {
	_, _, code := executeAgainst(t, "", "mcp", "list", "--framework", "hermes")
	if code != output.ExitError {
		t.Errorf("exit code = %d, want %d (unknown flag)", code, output.ExitError)
	}
}

// A show on a missing slug is exit 3 with the exact did-you-mean envelope —
// the load-bearing agent contract for typo recovery.
func TestShowNotFoundDidYouMean(t *testing.T) {
	srv := newMockServer(t)

	t.Run("soul typo", func(t *testing.T) {
		stdout, stderr, code := executeAgainst(t, srv.URL, "soul", "show", "sherloc", "--json")
		if code != output.ExitNotFound {
			t.Fatalf("exit code = %d, want %d", code, output.ExitNotFound)
		}
		if stdout != "" {
			t.Errorf("stdout must stay clean on error, got %q", stdout)
		}
		want := `{"error":{"code":"not_found","message":"soul \"sherloc\" not found","hint":"did you mean \"sherlock\"? Run: positronick soul list"}}` + "\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})

	t.Run("listing fragment", func(t *testing.T) {
		_, stderr, code := executeAgainst(t, srv.URL, "mcp", "show", "grafana", "--json")
		if code != output.ExitNotFound {
			t.Fatalf("exit code = %d, want %d", code, output.ExitNotFound)
		}
		want := `{"error":{"code":"not_found","message":"mcp \"grafana\" not found","hint":"did you mean \"grafana-mcp\"? Run: positronick mcp list"}}` + "\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})

	t.Run("no plausible suggestion still points at list", func(t *testing.T) {
		_, stderr, code := executeAgainst(t, srv.URL, "soul", "show", "zzz", "--json")
		if code != output.ExitNotFound {
			t.Fatalf("exit code = %d, want %d", code, output.ExitNotFound)
		}
		want := `{"error":{"code":"not_found","message":"soul \"zzz\" not found","hint":"Run: positronick soul list"}}` + "\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})
}

// The slug namespace is shared across listing types; a slug that exists but
// belongs to another noun is "not found" for this noun, with a redirecting
// hint, so `mcp show github-cli` never renders a CLI as an MCP server.
func TestShowWrongTypeIsNotFound(t *testing.T) {
	srv := newMockServer(t)
	_, stderr, code := executeAgainst(t, srv.URL, "mcp", "show", "github-cli", "--json")
	if code != output.ExitNotFound {
		t.Fatalf("exit code = %d, want %d", code, output.ExitNotFound)
	}
	want := `{"error":{"code":"not_found","message":"mcp \"github-cli\" not found","hint":"\"github-cli\" is a cli — run: positronick cli show github-cli"}}` + "\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// A connection failure is a plain exit-1 error, not a panic and not exit 3.
func TestNetworkErrorExitsOne(t *testing.T) {
	// Reserve a port and close it so the dial is refused deterministically.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadURL := "http://" + l.Addr().String()
	_ = l.Close()

	stdout, stderr, code := executeAgainst(t, deadURL, "soul", "list", "--json")
	if code != output.ExitError {
		t.Errorf("exit code = %d, want %d", code, output.ExitError)
	}
	if stdout != "" {
		t.Errorf("stdout must stay clean on error, got %q", stdout)
	}
	if !strings.HasPrefix(stderr, `{"error":{"code":"error",`) {
		t.Errorf("stderr = %q, want a generic JSON error envelope", stderr)
	}
}

// --raw prints the SOUL.md body verbatim and nothing else — even when --json
// is also set — and it must come from the JSON detail endpoint, never the .md
// endpoint (which bumps the install counter; the mock 418s on .md to enforce
// this).
func TestSoulShowRaw(t *testing.T) {
	srv := newMockServer(t)
	want := mockapi.Souls[0].Content

	for _, args := range [][]string{
		{"soul", "show", "sherlock", "--raw"},
		{"soul", "show", "sherlock", "--raw", "--json"},
	} {
		stdout, _, code := executeAgainst(t, srv.URL, args...)
		if code != 0 {
			t.Fatalf("%v exit code = %d, want 0", args, code)
		}
		if stdout != want {
			t.Errorf("%v stdout = %q, want the verbatim body %q", args, stdout, want)
		}
	}
}

// Human show keeps stdout pure data: the --raw hint goes to stderr, and
// --quiet removes it.
func TestSoulShowHintOnStderr(t *testing.T) {
	srv := newMockServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL, "soul", "show", "sherlock")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr, "--raw") {
		t.Errorf("stderr = %q, want the --raw hint", stderr)
	}
	if strings.Contains(stdout, "--raw") {
		t.Errorf("stdout = %q, must not carry the hint", stdout)
	}

	_, stderr, _ = executeAgainst(t, srv.URL, "soul", "show", "sherlock", "--quiet")
	if stderr != "" {
		t.Errorf("--quiet stderr = %q, want empty", stderr)
	}
}

// Search relevance must match the website: earlier substring positions rank
// higher, and --limit truncates after ranking.
func TestSearchRankingAndLimit(t *testing.T) {
	srv := newMockServer(t)

	stdout, _, code := executeAgainst(t, srv.URL, "soul", "search", "o", "--limit", "2", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	// "o" sits at index 1 in mOriarty, 4 in watsOn, 5 in sherlOck.
	if !strings.Contains(stdout, `"count": 2`) {
		t.Errorf("stdout = %q, want count 2 after --limit 2", stdout)
	}
	mor := strings.Index(stdout, `"slug": "moriarty"`)
	wat := strings.Index(stdout, `"slug": "watson"`)
	if mor == -1 || wat == -1 || mor > wat {
		t.Errorf("ranking order wrong: moriarty@%d watson@%d in %q", mor, wat, stdout)
	}
	if strings.Contains(stdout, `"slug": "sherlock"`) {
		t.Errorf("stdout = %q, sherlock must be cut by --limit 2", stdout)
	}
}

// Category and framework filters are case-insensitive: CLI users type
// freehand, while the site picks from a dropdown.
func TestFiltersAreCaseInsensitive(t *testing.T) {
	srv := newMockServer(t)

	stdout, _, code := executeAgainst(t, srv.URL, "soul", "list", "--category", "technical", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, `"count": 1`) || !strings.Contains(stdout, `"slug": "sherlock"`) {
		t.Errorf("stdout = %q, want only sherlock (category Technical)", stdout)
	}

	stdout, _, code = executeAgainst(t, srv.URL, "soul", "list", "--framework", "HERMES", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, `"count": 2`) {
		t.Errorf("stdout = %q, want sherlock+watson (framework hermes)", stdout)
	}
}

// Every listing noun must pass its type to the API so the server filters.
func TestListingNounSendsItsType(t *testing.T) {
	srv := newMockServer(t)
	stdout, _, code := executeAgainst(t, srv.URL, "harness", "list", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, `"slug": "claude-code"`) || strings.Contains(stdout, `"slug": "github-cli"`) {
		t.Errorf("stdout = %q, want only harness listings", stdout)
	}
}

// cobra's built-in completion command must stay available for shell setup.
func TestCompletionBash(t *testing.T) {
	stdout, _, code := executeAgainst(t, "", "completion", "bash")
	if code != 0 {
		t.Fatalf("completion bash exit code = %d, want 0", code)
	}
	if stdout == "" {
		t.Error("completion bash must print a script")
	}
}

// An unknown noun stays a plain cobra unknown-command error (exit 1).
func TestUnknownNoun(t *testing.T) {
	_, stderr, code := executeAgainst(t, "", "daemon", "list", "--json")
	if code != output.ExitError {
		t.Errorf("exit code = %d, want %d", code, output.ExitError)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Errorf("stderr = %q, want cobra's unknown command error", stderr)
	}
}
