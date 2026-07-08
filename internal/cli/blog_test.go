package cli

import (
	"strings"
	"testing"

	"github.com/positronick/cli/internal/mockapi"
	"github.com/positronick/cli/internal/output"
)

// blog list returns every published post newest-first; --json pins the
// {count,posts} contract.
func TestBlogListNewestFirst(t *testing.T) {
	srv := newMockServer(t)
	stdout, _, code := executeAgainst(t, srv.URL, "blog", "list", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, `"count": 3`) {
		t.Errorf("stdout = %q, want count 3", stdout)
	}
	// Newest-first: openclaw (06-10) precedes the cli release (06-01) precedes the intro article (05-20).
	oc := strings.Index(stdout, `"slug": "openclaw-joins-the-registry"`)
	cli := strings.Index(stdout, `"slug": "positronick-cli-v0-1-0"`)
	intro := strings.Index(stdout, `"slug": "introducing-positronick"`)
	if oc == -1 || cli == -1 || intro == -1 || oc >= cli || cli >= intro {
		t.Errorf("not newest-first: openclaw@%d cli@%d intro@%d in %q", oc, cli, intro, stdout)
	}
}

// --kind narrows server-side to one post kind; an unknown kind is rejected
// before any network call, with the valid set named.
func TestBlogListKindFilter(t *testing.T) {
	srv := newMockServer(t)
	stdout, _, code := executeAgainst(t, srv.URL, "blog", "list", "--kind", "release", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, `"count": 1`) || !strings.Contains(stdout, `"slug": "positronick-cli-v0-1-0"`) {
		t.Errorf("stdout = %q, want only the release post", stdout)
	}

	// Point at a dead port: validation must reject before any request.
	_, stderr, code := executeAgainst(t, "http://127.0.0.1:1", "blog", "list", "--kind", "essay")
	if code != output.ExitError {
		t.Errorf("exit code = %d, want %d", code, output.ExitError)
	}
	if want := `invalid --kind "essay" (valid: article, release, link)`; !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// --raw prints the markdown body verbatim and nothing else — even when --json
// is also set — and it comes from the .md endpoint (the frontmatter the JSON
// `content` lacks proves the source).
func TestBlogShowRaw(t *testing.T) {
	srv := newMockServer(t)

	var want string
	for _, p := range mockapi.Posts {
		if p.Slug == "positronick-cli-v0-1-0" {
			want = mockapi.PostMarkdown(p)
		}
	}
	if !strings.HasPrefix(want, "---\n") {
		t.Fatalf("fixture markdown should carry frontmatter, got %q", want)
	}

	for _, args := range [][]string{
		{"blog", "show", "positronick-cli-v0-1-0", "--raw"},
		{"blog", "show", "positronick-cli-v0-1-0", "--raw", "--json"},
	} {
		stdout, _, code := executeAgainst(t, srv.URL, args...)
		if code != 0 {
			t.Fatalf("%v exit code = %d, want 0", args, code)
		}
		if stdout != want {
			t.Errorf("%v stdout = %q, want the verbatim .md body %q", args, stdout, want)
		}
	}
}

// A show on a missing slug is exit 3 with the exact did-you-mean envelope — the
// load-bearing agent contract for typo recovery.
func TestBlogShowNotFoundDidYouMean(t *testing.T) {
	srv := newMockServer(t)

	t.Run("typo gets a suggestion", func(t *testing.T) {
		stdout, stderr, code := executeAgainst(t, srv.URL, "blog", "show", "positronik-cli-v0-1-0", "--json")
		if code != output.ExitNotFound {
			t.Fatalf("exit code = %d, want %d", code, output.ExitNotFound)
		}
		if stdout != "" {
			t.Errorf("stdout must stay clean on error, got %q", stdout)
		}
		want := `{"error":{"code":"not_found","message":"post \"positronik-cli-v0-1-0\" not found","hint":"did you mean \"positronick-cli-v0-1-0\"? Run: positronick blog list"}}` + "\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})

	t.Run("no plausible suggestion still points at list", func(t *testing.T) {
		_, stderr, code := executeAgainst(t, srv.URL, "blog", "show", "zzzzzzzzz", "--json")
		if code != output.ExitNotFound {
			t.Fatalf("exit code = %d, want %d", code, output.ExitNotFound)
		}
		want := `{"error":{"code":"not_found","message":"post \"zzzzzzzzz\" not found","hint":"Run: positronick blog list"}}` + "\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})
}

// Human show keeps stdout pure data: the --raw hint goes to stderr, and --quiet
// removes it.
func TestBlogShowHintOnStderr(t *testing.T) {
	srv := newMockServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL, "blog", "show", "positronick-cli-v0-1-0")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr, "--raw") {
		t.Errorf("stderr = %q, want the --raw hint", stderr)
	}
	if strings.Contains(stdout, "--raw") {
		t.Errorf("stdout = %q, must not carry the hint", stdout)
	}

	_, stderr, _ = executeAgainst(t, srv.URL, "blog", "show", "positronick-cli-v0-1-0", "--quiet")
	if stderr != "" {
		t.Errorf("--quiet stderr = %q, want empty", stderr)
	}
}
