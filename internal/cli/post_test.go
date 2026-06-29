package cli

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/mockapi"
	"github.com/positronick/cli/internal/output"
)

// post create from a markdown fixture: 201, the --json shape and the human
// summary (public path included) pinned as goldens. The default status is the
// server's draft — an agent post never self-publishes.
func TestPostCreateGolden(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"post", "create", "--file", "testdata/new-post.md", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "post-create.json", stdout)
		if !strings.Contains(stdout, `"status": "draft"`) {
			t.Errorf("stdout = %q, want the post defaulting to draft", stdout)
		}
	})

	t.Run("human", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t) // fresh state: the slug is free again
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"post", "create", "--file", "testdata/new-post.md")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "post-create.txt", stdout)
		if !strings.Contains(stdout, "/blog/the-positronick-manifesto") {
			t.Errorf("stdout = %q, want the public URL path", stdout)
		}
		// The draft hint rides stderr so the stdout golden stays clean.
		if !strings.Contains(stderr, "still a draft") {
			t.Errorf("stderr = %q, want the publish-it hint for a draft", stderr)
		}
	})
}

// status defaults to draft on create unless --status is given — the body the
// CLI sends never forces publish, so the server's default stands.
func TestPostCreateDefaultsDraft(t *testing.T) {
	adminEnv(t)
	srv, last := newCaptureServer(t)
	_, stderr, code := executeAgainst(t, srv.URL,
		"post", "create", "--file", "testdata/new-post.md", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var sent map[string]any
	if err := json.Unmarshal(last.body, &sent); err != nil {
		t.Fatalf("sent body is not JSON: %v", err)
	}
	if _, forced := sent["status"]; forced {
		t.Errorf("sent = %v, want no status field so the server's draft default stands", sent)
	}
}

// Flags override frontmatter, the server-assigned fields are stripped, and the
// body rides as content — asserted on the exact JSON the mock received.
func TestPostCreateFlagsOverrideFrontmatter(t *testing.T) {
	adminEnv(t)
	srv, last := newCaptureServer(t)

	_, stderr, code := executeAgainst(t, srv.URL,
		"post", "create", "--file", "testdata/new-post.md",
		"--title", "A New Title", "--slug", "a-new-slug", "--kind", "link",
		"--tag", "news", "--author-handle", "nsollazzo", "--status", "published", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if last.method != http.MethodPost || last.path != "/api/admin/posts" {
		t.Fatalf("request = %s %s, want POST /api/admin/posts", last.method, last.path)
	}
	var sent map[string]any
	if err := json.Unmarshal(last.body, &sent); err != nil {
		t.Fatalf("sent body is not JSON: %v", err)
	}
	if sent["title"] != "A New Title" || sent["slug"] != "a-new-slug" ||
		sent["kind"] != "link" || sent["status"] != "published" || sent["authorHandle"] != "nsollazzo" {
		t.Errorf("sent = %v, want the flag values to beat the frontmatter", sent)
	}
	if !reflect.DeepEqual(sent["tags"], []any{"news"}) {
		t.Errorf("tags = %v, want the --tag override", sent["tags"])
	}
	if c, _ := sent["content"].(string); !strings.Contains(c, "# The Positronick Manifesto") {
		t.Errorf("content = %q, want the markdown body", c)
	}
	// Untouched frontmatter passes through; server-assigned fields never do.
	if sent["excerpt"] == nil || sent["category"] != "Announcements" {
		t.Errorf("sent = %v, want unflagged frontmatter fields verbatim", sent)
	}
	for _, dropped := range []string{"id", "createdAt", "updatedAt", "contentHash", "viewCount"} {
		if _, ok := sent[dropped]; ok {
			t.Errorf("sent body must not carry server-assigned %q", dropped)
		}
	}
}

// A 422 is the server validator speaking: its message must reach the user
// verbatim. An invalid --kind is the cheapest way to provoke one.
func TestPostCreate422Verbatim(t *testing.T) {
	const msg = `admin post create: invalid kind "bogus" (expected one of: article, release, link)`
	adminEnv(t)
	srv := newMockServer(t)
	stdout, stderr, code := executeAgainst(t, srv.URL,
		"post", "create", "--file", "testdata/new-post.md", "--kind", "bogus", "--json")
	if code != output.ExitError {
		t.Fatalf("exit code = %d, want %d", code, output.ExitError)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on error", stdout)
	}
	want := `{"error":{"code":"invalid_input","message":"` + strings.ReplaceAll(msg, `"`, `\"`) + `"}}` + "\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// 401 (no credentials) and 403 (authenticated non-admin) both map to exit 4
// with the admin-login hint — the server is the authorization authority.
func TestPostAdminAuthErrors(t *testing.T) {
	t.Run("401 logged out", func(t *testing.T) {
		isolateAuth(t)
		srv := newMockServer(t)
		_, stderr, code := executeAgainst(t, srv.URL,
			"post", "create", "--file", "testdata/new-post.md", "--json")
		if code != output.ExitAuth {
			t.Fatalf("exit code = %d, want %d", code, output.ExitAuth)
		}
		want := `{"error":{"code":"auth_required","message":"authentication required — run ` +
			"`positronick login`" + `","hint":"run positronick login with an admin account"}}` + "\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})

	t.Run("403 non-admin", func(t *testing.T) {
		dir := isolateAuth(t)
		srv := newMockServer(t)
		seedLogin(t, dir, srv.URL, mockapi.PlebToken)
		_, stderr, code := executeAgainst(t, srv.URL,
			"post", "list", "--json")
		if code != output.ExitAuth {
			t.Fatalf("exit code = %d, want %d", code, output.ExitAuth)
		}
		want := `{"error":{"code":"auth_required","message":"admin access required",` +
			`"hint":"run positronick login with an admin account"}}` + "\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})
}

// Updating a feed-mirrored post by SLUG: the ref 404s as an id, resolves
// through the public blog detail endpoint, and the patch flips ownership —
// tookOwnership in the JSON contract, a WARNING on stderr for humans.
func TestPostUpdateBySlugTookOwnership(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"post", "update", "positronick-cli-v0-1-0", "--excerpt", "Now with admin write commands.", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "post-update-ownership.json", stdout)
		if !strings.Contains(stdout, `"tookOwnership": true`) {
			t.Errorf("stdout = %q, want tookOwnership true for a feed-owned row", stdout)
		}
	})

	t.Run("human warning", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"post", "update", "positronick-cli-v0-1-0", "--excerpt", "Now with admin write commands.")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "post-update-ownership.txt", stdout)
		if !strings.Contains(stderr, "WARNING: took ownership from the feed") {
			t.Errorf("stderr = %q, want the ownership warning", stderr)
		}
		if !strings.Contains(stderr, "--source feed") {
			t.Errorf("stderr = %q, want the hand-back hint", stderr)
		}
	})

	t.Run("explicit --source feed does not take ownership", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"post", "update", "positronick-cli-v0-1-0", "--excerpt", "tweak", "--source", "feed")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		if strings.Contains(stderr, "WARNING") {
			t.Errorf("stderr = %q, want no warning when ownership stays with the feed", stderr)
		}
		if !strings.Contains(stdout, "Updated post positronick-cli-v0-1-0") {
			t.Errorf("stdout = %q, want the update summary", stdout)
		}
	})
}

// An update by id PATCHes directly — no slug resolution round-trip — and a ref
// that is neither id nor known slug is exit 3.
func TestPostUpdateResolution(t *testing.T) {
	t.Run("by id", func(t *testing.T) {
		adminEnv(t)
		srv, last := newCaptureServer(t)
		_, stderr, code := executeAgainst(t, srv.URL,
			"post", "update", "01POSTINTRO0000000000000XX", "--title", "Introducing Positronick (v2)", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		if last.path != "/api/admin/posts/01POSTINTRO0000000000000XX" {
			t.Errorf("path = %q, want the id PATCHed directly", last.path)
		}
		if string(last.body) != `{"title":"Introducing Positronick (v2)"}` {
			t.Errorf("body = %s, want only the provided field", last.body)
		}
	})

	t.Run("unknown ref is exit 3", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"post", "update", "no-such-post", "--title", "x", "--json")
		if code != output.ExitNotFound {
			t.Fatalf("exit code = %d, want %d", code, output.ExitNotFound)
		}
		if stdout != "" {
			t.Errorf("stdout = %q, want empty on error", stdout)
		}
		if !strings.Contains(stderr, `"code":"not_found"`) ||
			!strings.Contains(stderr, `post \"no-such-post\" not found`) {
			t.Errorf("stderr = %q, want the not-found envelope", stderr)
		}
	})

	t.Run("no fields is a client-side error", func(t *testing.T) {
		adminEnv(t)
		// Unreachable server: the validation must fire before any request.
		_, stderr, code := executeAgainst(t, "http://127.0.0.1:1", "post", "update", "positronick-cli-v0-1-0")
		if code != output.ExitError {
			t.Fatalf("exit code = %d, want %d", code, output.ExitError)
		}
		if !strings.Contains(stderr, "nothing to update") {
			t.Errorf("stderr = %q, want the nothing-to-update error", stderr)
		}
	})
}

// There is no delete verb: unpublishing is PATCH {"status":"draft"}, by slug.
// The article post is api-owned, so this does not flip ownership.
func TestPostUnpublishViaStatusDraft(t *testing.T) {
	adminEnv(t)
	srv, last := newCaptureServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL,
		"post", "update", "introducing-positronick", "--status", "draft", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if last.path != "/api/admin/posts/01POSTINTRO0000000000000XX" {
		t.Errorf("path = %q, want the slug resolved to the post id", last.path)
	}
	if string(last.body) != `{"status":"draft"}` {
		t.Errorf("body = %s, want exactly the status patch", last.body)
	}
	if !strings.Contains(stdout, `"status": "draft"`) {
		t.Errorf("stdout = %q, want the post unpublished", stdout)
	}
	if strings.Contains(stderr, "WARNING") {
		t.Errorf("stderr = %q, want no ownership warning for an api-owned post", stderr)
	}
}

// post list returns every post, any status, with source — the --json and human
// table both pinned as goldens.
func TestPostListGolden(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL, "post", "list", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "post-list.json", stdout)
		if !strings.Contains(stdout, `"source": "feed"`) {
			t.Errorf("stdout = %q, want the source marker in the list", stdout)
		}
	})

	t.Run("human", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL, "post", "list")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "post-list.txt", stdout)
	})
}
