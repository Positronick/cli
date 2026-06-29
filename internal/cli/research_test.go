package cli

import (
	"strings"
	"testing"

	"github.com/positronick/cli/internal/output"
)

// The default feed returns every published post newest-first and reports the
// newest publishedAt as `latest` — the value an agent feeds back as --since.
func TestResearchDefaultFeed(t *testing.T) {
	srv := newMockServer(t)
	stdout, _, code := executeAgainst(t, srv.URL, "research", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, `"count": 3`) {
		t.Errorf("stdout = %q, want count 3", stdout)
	}
	if !strings.Contains(stdout, `"latest": "2026-06-10T08:00:00.000Z"`) {
		t.Errorf("stdout = %q, want latest = newest publishedAt", stdout)
	}
	// Newest-first: openclaw-launch (06-10) precedes hermes (06-01) precedes the article (05-20).
	oc := strings.Index(stdout, `"slug": "openclaw-launch"`)
	hm := strings.Index(stdout, `"slug": "hermes-v2-1-0"`)
	ar := strings.Index(stdout, `"slug": "shipping-the-cli"`)
	if oc == -1 || hm == -1 || ar == -1 || oc >= hm || hm >= ar {
		t.Errorf("not newest-first: openclaw@%d hermes@%d article@%d in %q", oc, hm, ar, stdout)
	}
}

// --since returns only the strictly-newer delta, but `latest` stays the feed's
// high-water mark (ignoring --since) so polling converges instead of looping.
func TestResearchSinceReturnsDelta(t *testing.T) {
	srv := newMockServer(t)
	stdout, _, code := executeAgainst(t, srv.URL,
		"research", "--since", "2026-06-01T09:00:00.000Z", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, `"count": 1`) || !strings.Contains(stdout, `"slug": "openclaw-launch"`) {
		t.Errorf("stdout = %q, want only the post newer than --since", stdout)
	}
	if strings.Contains(stdout, `"slug": "hermes-v2-1-0"`) {
		t.Errorf("stdout = %q, the --since boundary post must be excluded (strictly-after)", stdout)
	}
	if !strings.Contains(stdout, `"latest": "2026-06-10T08:00:00.000Z"`) {
		t.Errorf("stdout = %q, latest must be the feed high-water mark, not the delta's", stdout)
	}
}

// --kind narrows server-side; an empty filter still reports a `latest` of null,
// never a crash.
func TestResearchKindFilter(t *testing.T) {
	srv := newMockServer(t)

	stdout, _, code := executeAgainst(t, srv.URL, "research", "--kind", "release", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, `"count": 1`) || !strings.Contains(stdout, `"slug": "hermes-v2-1-0"`) {
		t.Errorf("stdout = %q, want only the release post", stdout)
	}

	stdout, _, code = executeAgainst(t, srv.URL, "research", "--category", "nope", "--json")
	if code != 0 {
		t.Fatalf("empty-match exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, `"count": 0`) || !strings.Contains(stdout, `"latest": null`) {
		t.Errorf("stdout = %q, want count 0 and latest null on an empty match", stdout)
	}
}

// Flags are validated before any network call, with the valid set named.
func TestResearchInvalidFlags(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantMessage string
	}{
		{
			"unknown kind",
			[]string{"research", "--kind", "essay"},
			`invalid --kind "essay" (valid: article, release, link)`,
		},
		{
			"limit too high",
			[]string{"research", "--limit", "500"},
			"--limit must be between 1 and 100, got 500",
		},
		{
			"zero limit",
			[]string{"research", "--limit", "0"},
			"--limit must be between 1 and 100, got 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Point at a dead port: validation must reject before any request.
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

// research is a read command: it must never require auth, and the human view
// keeps stdout pure data while the `latest` poll hint rides stderr.
func TestResearchHumanHintOnStderr(t *testing.T) {
	srv := newMockServer(t)
	stdout, stderr, code := executeAgainst(t, srv.URL, "research")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr, "--since") || !strings.Contains(stderr, "latest:") {
		t.Errorf("stderr = %q, want the latest/--since poll hint", stderr)
	}
	if strings.Contains(stdout, "latest:") {
		t.Errorf("stdout = %q, must stay pure tabular data", stdout)
	}
}
