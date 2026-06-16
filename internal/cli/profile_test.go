package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/output"
)

// profile create from flags: 201 with the --json shape and the human summary
// (public profile path included) pinned as goldens.
func TestProfileCreateGolden(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"profile", "create", "--handle", "shadcn", "--name", "shadcn", "--kind", "person", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "profile-create.json", stdout)
	})

	t.Run("human", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"profile", "create", "--handle", "shadcn", "--name", "shadcn", "--kind", "person")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "profile-create.txt", stdout)
		if !strings.Contains(stdout, "/profiles/shadcn") {
			t.Errorf("stdout = %q, want the public profile path", stdout)
		}
	})
}

// Empty optional fields are omitted from the wire so the server applies its
// defaults; the created row comes back with the GitHub-convention avatar and
// source=api.
func TestProfileCreateDefaultsAvatar(t *testing.T) {
	adminEnv(t)
	srv, last := newCaptureServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL,
		"profile", "create", "--handle", "shadcn", "--name", "shadcn", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if last.method != http.MethodPost || last.path != "/api/admin/profiles" {
		t.Fatalf("request = %s %s, want POST /api/admin/profiles", last.method, last.path)
	}
	var sent map[string]any
	if err := json.Unmarshal(last.body, &sent); err != nil {
		t.Fatalf("sent body is not JSON: %v", err)
	}
	if _, ok := sent["avatarUrl"]; ok {
		t.Errorf("sent body must omit an empty avatarUrl (server defaults it), got %v", sent["avatarUrl"])
	}
	if sent["verified"] != false || sent["official"] != false {
		t.Errorf("sent = %v, want verified/official defaulting to false", sent)
	}

	var resp struct {
		Profile struct {
			AvatarURL string `json:"avatarUrl"`
			Source    string `json:"source"`
		} `json:"profile"`
	}
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if resp.Profile.AvatarURL != "https://github.com/shadcn.png" {
		t.Errorf("avatarUrl = %q, want the GitHub-convention default", resp.Profile.AvatarURL)
	}
	if resp.Profile.Source != "api" {
		t.Errorf("source = %q, want api for an admin-created profile", resp.Profile.Source)
	}
}

// A duplicate handle is a 409 conflict → exit 1 with the server's message.
func TestProfileCreateDuplicateHandle(t *testing.T) {
	adminEnv(t)
	srv := newMockServer(t)
	// "anthropic" is a seeded curated author.
	_, stderr, code := executeAgainst(t, srv.URL,
		"profile", "create", "--handle", "anthropic", "--name", "Anthropic", "--json")
	if code != output.ExitError {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, output.ExitError, stderr)
	}
	if !strings.Contains(stderr, `"code":"conflict"`) || !strings.Contains(stderr, "already taken") {
		t.Errorf("stderr = %q, want the conflict envelope", stderr)
	}
}

// profile list returns every profile (seeded authors + any api-created),
// sorted by handle; the --json and human layouts are pinned.
func TestProfileListGolden(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL, "profile", "list", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "profile-list.json", stdout)
	})

	t.Run("human", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL, "profile", "list")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "profile-list.txt", stdout)
	})
}

// The whole point of the feature: create a profile, then author a listing under
// it. Before profile create existed, the listing create 422'd on the unknown
// handle (profiles were git-curated only). Both run against one server so the
// created profile persists into the listing create.
func TestProfileCreateThenListing(t *testing.T) {
	adminEnv(t)
	srv := newMockServer(t)

	_, stderr, code := executeAgainst(t, srv.URL,
		"profile", "create", "--handle", "shadcn", "--name", "shadcn", "--json")
	if code != 0 {
		t.Fatalf("profile create exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	_, stderr, code = executeAgainst(t, srv.URL,
		"listing", "create", "--file", "testdata/new-listing.json", "--profile", "shadcn", "--json")
	if code != 0 {
		t.Fatalf("listing create under the new profile exit = %d, want 0 (stderr: %s)", code, stderr)
	}
}
