package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/mockapi"
	"github.com/positronick/cli/internal/output"
)

// feed list renders the seeded feeds, sorted by label for stable output. The
// --json shape and the human table are both pinned as goldens.
func TestFeedListGolden(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL, "feed", "list", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "feed-list.json", stdout)
	})

	t.Run("human", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL, "feed", "list")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "feed-list.txt", stdout)
	})
}

// feed create: required + optional fields go on the wire as the field map, the
// repeatable --tag becomes defaultTags, and the --json shape is pinned.
func TestFeedCreate(t *testing.T) {
	adminEnv(t)
	srv, last := newCaptureServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL,
		"feed", "create",
		"--label", "Vite Releases",
		"--feed-url", "https://github.com/vitejs/vite",
		"--kind", "github_release",
		"--category", "Releases",
		"--author", "nsollazzo",
		"--tag", "release", "--tag", "frontend",
		"--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	assertGolden(t, "feed-create.json", stdout)

	if last.method != http.MethodPost || last.path != "/api/admin/feeds" {
		t.Fatalf("request = %s %s, want POST /api/admin/feeds", last.method, last.path)
	}
	var sent map[string]any
	if err := json.Unmarshal(last.body, &sent); err != nil {
		t.Fatalf("sent body is not JSON: %v", err)
	}
	if sent["label"] != "Vite Releases" || sent["kind"] != "github_release" {
		t.Errorf("sent = %v, want the field values", sent)
	}
	if sent["authorHandle"] != "nsollazzo" {
		t.Errorf("authorHandle = %v, want the --author value", sent["authorHandle"])
	}
	if tags, _ := sent["defaultTags"].([]any); len(tags) != 2 || tags[0] != "release" || tags[1] != "frontend" {
		t.Errorf("defaultTags = %v, want the repeatable --tag values", sent["defaultTags"])
	}
	// The bool defaults are always sent on create (the server's defaults).
	if sent["autoPublish"] != false || sent["enabled"] != true {
		t.Errorf("sent = %v, want autoPublish false + enabled true", sent)
	}
	// An unset optional (no --listing) is omitted, not sent empty.
	if _, ok := sent["listingSlug"]; ok {
		t.Errorf("sent body must omit an unset listingSlug, got %v", sent["listingSlug"])
	}
}

// An invalid kind is the server validator speaking: its message must reach the
// user verbatim in the error envelope.
func TestFeedCreateInvalidKind(t *testing.T) {
	adminEnv(t)
	srv := newMockServer(t)
	_, stderr, code := executeAgainst(t, srv.URL,
		"feed", "create", "--label", "X", "--feed-url", "https://example.com/x",
		"--kind", "atom", "--category", "Releases", "--json")
	if code != output.ExitError {
		t.Fatalf("exit code = %d, want %d", code, output.ExitError)
	}
	want := `{"error":{"code":"invalid_input","message":"admin feed create: invalid kind \"atom\" (expected one of: github_release, rss)"}}` + "\n"
	if stderr != want {
		t.Errorf("stderr = %q, want the validator message verbatim", stderr)
	}
}

// --enabled=false pauses a feed: the PATCH body carries exactly that field and
// the feed comes back paused.
func TestFeedUpdatePause(t *testing.T) {
	adminEnv(t)
	srv, last := newCaptureServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL,
		"feed", "update", mockapi.FeedReleaseID, "--enabled=false", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if last.method != http.MethodPatch || last.path != "/api/admin/feeds/"+mockapi.FeedReleaseID {
		t.Errorf("request = %s %s, want the feed PATCHed by id", last.method, last.path)
	}
	if string(last.body) != `{"enabled":false}` {
		t.Errorf("body = %s, want exactly the enabled patch", last.body)
	}
	assertGolden(t, "feed-update.json", stdout)
	if !strings.Contains(stdout, `"enabled": false`) {
		t.Errorf("stdout = %q, want the feed paused", stdout)
	}
}

// An update with no field flags is a client-side error, before any request.
func TestFeedUpdateNothing(t *testing.T) {
	adminEnv(t)
	_, stderr, code := executeAgainst(t, "http://127.0.0.1:1", "feed", "update", mockapi.FeedReleaseID)
	if code != output.ExitError {
		t.Fatalf("exit code = %d, want %d", code, output.ExitError)
	}
	if !strings.Contains(stderr, "nothing to update") {
		t.Errorf("stderr = %q, want the nothing-to-update error", stderr)
	}
}

// feed sync surfaces the ingest summary; the --json shape is pinned.
func TestFeedSyncGolden(t *testing.T) {
	adminEnv(t)
	srv := newMockServer(t)
	stdout, stderr, code := executeAgainst(t, srv.URL, "feed", "sync", mockapi.FeedReleaseID, "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	assertGolden(t, "feed-sync.json", stdout)
}

// A fetch/parse failure is the server's 502 ({"summary"} with error set) — the
// CLI surfaces the reason as a clear error (exit 1), not a bare "Bad Gateway".
func TestFeedSync502ClearError(t *testing.T) {
	adminEnv(t)
	srv := newMockServer(t)

	// Create a github_release feed whose URL is not a GitHub repo, so the mock
	// ingest fails the same way production's parseGithubRepoUrl does.
	out, stderr, code := executeAgainst(t, srv.URL,
		"feed", "create", "--label", "Broken", "--feed-url", "https://example.com/not-a-repo",
		"--kind", "github_release", "--category", "Releases", "--json")
	if code != 0 {
		t.Fatalf("create exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var created struct {
		Feed struct {
			ID string `json:"id"`
		} `json:"feed"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("decoding create output: %v", err)
	}

	_, stderr, code = executeAgainst(t, srv.URL, "feed", "sync", created.Feed.ID, "--json")
	if code != output.ExitError {
		t.Fatalf("exit code = %d, want %d", code, output.ExitError)
	}
	want := `{"error":{"code":"error","message":"feed sync failed: not a GitHub repo URL: https://example.com/not-a-repo"}}` + "\n"
	if stderr != want {
		t.Errorf("stderr = %q, want the recovered failure reason", stderr)
	}
}

// A missing feed id is exit 3, the standard not-found envelope.
func TestFeedSyncNotFound(t *testing.T) {
	adminEnv(t)
	srv := newMockServer(t)
	_, stderr, code := executeAgainst(t, srv.URL, "feed", "sync", "01NOSUCHFEED00000000000000", "--json")
	if code != output.ExitNotFound {
		t.Fatalf("exit code = %d, want %d", code, output.ExitNotFound)
	}
	if !strings.Contains(stderr, `"code":"not_found"`) {
		t.Errorf("stderr = %q, want the not-found envelope", stderr)
	}
}

// A non-admin login is exit 4 on a feed command — the server is the authority,
// the CLI relays its 403 through the admin-login hint.
func TestFeedListNonAdmin(t *testing.T) {
	dir := isolateAuth(t)
	srv := newMockServer(t)
	seedLogin(t, dir, srv.URL, mockapi.PlebToken)
	_, stderr, code := executeAgainst(t, srv.URL, "feed", "list", "--json")
	if code != output.ExitAuth {
		t.Fatalf("exit code = %d, want %d", code, output.ExitAuth)
	}
	if !strings.Contains(stderr, "admin access required") {
		t.Errorf("stderr = %q, want the admin-required envelope", stderr)
	}
}
