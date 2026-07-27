package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/output"
)

// feed list against a fresh mock (no seed feeds): empty --json and human
// table are pinned as goldens.
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

// feed create against a seeded listing/author (claude-code / anthropic): the
// --json shape and human summary are pinned as goldens.
func TestFeedCreateGolden(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"feed", "create",
			"--label", "Claude Code",
			"--url", "https://github.com/anthropics/claude-code",
			"--listing", "claude-code",
			"--author", "anthropic",
			"--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "feed-create.json", stdout)
	})

	t.Run("human", func(t *testing.T) {
		adminEnv(t)
		srv := newMockServer(t)
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"feed", "create",
			"--label", "Claude Code",
			"--url", "https://github.com/anthropics/claude-code",
			"--listing", "claude-code",
			"--author", "anthropic")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "feed-create.txt", stdout)
		if !strings.Contains(stdout, "claude-code") || !strings.Contains(stdout, "anthropic") {
			t.Errorf("stdout = %q, want listing and author in the summary", stdout)
		}
	})
}

// An unknown author is a 422 unknown_profile → exit 1 with the server's message.
func TestFeedCreateUnknownAuthor(t *testing.T) {
	adminEnv(t)
	srv := newMockServer(t)
	_, stderr, code := executeAgainst(t, srv.URL,
		"feed", "create",
		"--label", "Nobody",
		"--url", "https://github.com/nobody/repo",
		"--author", "nobody",
		"--json")
	if code != output.ExitError {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, output.ExitError, stderr)
	}
	if !strings.Contains(stderr, `"code":"unknown_profile"`) || !strings.Contains(stderr, "nobody") {
		t.Errorf("stderr = %q, want the unknown_profile envelope", stderr)
	}
}

// create then sync: summary is deterministic (mock never hits the network) and
// the feed's lastStatus becomes "ok" with a non-null lastFetchedAt.
func TestFeedSync(t *testing.T) {
	adminEnv(t)
	srv := newMockServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL,
		"feed", "create",
		"--label", "Claude Code",
		"--url", "https://github.com/anthropics/claude-code",
		"--listing", "claude-code",
		"--author", "anthropic",
		"--json")
	if code != 0 {
		t.Fatalf("create exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	var created struct {
		Feed struct {
			ID string `json:"id"`
		} `json:"feed"`
	}
	if err := json.Unmarshal([]byte(stdout), &created); err != nil {
		t.Fatalf("create response is not JSON: %v", err)
	}

	stdout, stderr, code = executeAgainst(t, srv.URL,
		"feed", "sync", created.Feed.ID, "--json")
	if code != 0 {
		t.Fatalf("sync exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	var synced struct {
		Summary struct {
			FeedID  string `json:"feedId"`
			Label   string `json:"label"`
			Fetched int    `json:"fetched"`
			Created int    `json:"created"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(stdout), &synced); err != nil {
		t.Fatalf("sync response is not JSON: %v", err)
	}
	if synced.Summary.FeedID != created.Feed.ID {
		t.Errorf("summary.feedId = %q, want %q", synced.Summary.FeedID, created.Feed.ID)
	}
	if synced.Summary.Fetched != 1 || synced.Summary.Created != 1 {
		t.Errorf("summary = %+v, want fetched=1 created=1", synced.Summary)
	}
	if synced.Summary.Label != "Claude Code" {
		t.Errorf("summary.label = %q, want Claude Code", synced.Summary.Label)
	}

	// After sync, list must show lastStatus=ok and a stamped lastFetchedAt.
	stdout, stderr, code = executeAgainst(t, srv.URL, "feed", "list", "--json")
	if code != 0 {
		t.Fatalf("list exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	var listed struct {
		Feeds []struct {
			ID            string  `json:"id"`
			LastStatus    *string `json:"lastStatus"`
			LastFetchedAt *string `json:"lastFetchedAt"`
		} `json:"feeds"`
	}
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("list response is not JSON: %v", err)
	}
	var found bool
	for _, f := range listed.Feeds {
		if f.ID != created.Feed.ID {
			continue
		}
		found = true
		if f.LastStatus == nil || *f.LastStatus != "ok" {
			t.Errorf("lastStatus = %v, want \"ok\" after sync", f.LastStatus)
		}
		if f.LastFetchedAt == nil || *f.LastFetchedAt == "" {
			t.Errorf("lastFetchedAt = %v, want non-null after sync", f.LastFetchedAt)
		}
	}
	if !found {
		t.Fatalf("feed %q not found in list after sync", created.Feed.ID)
	}
}

// create then update --disabled: the feed comes back enabled=false.
func TestFeedUpdateDisable(t *testing.T) {
	adminEnv(t)
	srv := newMockServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL,
		"feed", "create",
		"--label", "Claude Code",
		"--url", "https://github.com/anthropics/claude-code",
		"--listing", "claude-code",
		"--author", "anthropic",
		"--json")
	if code != 0 {
		t.Fatalf("create exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	var created struct {
		Feed struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
		} `json:"feed"`
	}
	if err := json.Unmarshal([]byte(stdout), &created); err != nil {
		t.Fatalf("create response is not JSON: %v", err)
	}
	if !created.Feed.Enabled {
		t.Fatal("created feed must start enabled")
	}

	stdout, stderr, code = executeAgainst(t, srv.URL,
		"feed", "update", created.Feed.ID, "--disabled", "--json")
	if code != 0 {
		t.Fatalf("update exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	var updated struct {
		Feed struct {
			Enabled bool `json:"enabled"`
		} `json:"feed"`
	}
	if err := json.Unmarshal([]byte(stdout), &updated); err != nil {
		t.Fatalf("update response is not JSON: %v", err)
	}
	if updated.Feed.Enabled {
		t.Error("feed.Enabled = true, want false after --disabled")
	}
}

// Empty --author / --listing on update clears attribution (null on the wire).
func TestFeedUpdateClearAttribution(t *testing.T) {
	adminEnv(t)
	srv := newMockServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL,
		"feed", "create",
		"--label", "Claude Code",
		"--url", "https://github.com/anthropics/claude-code",
		"--listing", "claude-code",
		"--author", "anthropic",
		"--json")
	if code != 0 {
		t.Fatalf("create exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	var created struct {
		Feed struct {
			ID           string  `json:"id"`
			AuthorHandle *string `json:"authorHandle"`
			ListingSlug  *string `json:"listingSlug"`
		} `json:"feed"`
	}
	if err := json.Unmarshal([]byte(stdout), &created); err != nil {
		t.Fatalf("create response is not JSON: %v", err)
	}
	if created.Feed.AuthorHandle == nil || created.Feed.ListingSlug == nil {
		t.Fatal("created feed must carry author and listing")
	}

	stdout, stderr, code = executeAgainst(t, srv.URL,
		"feed", "update", created.Feed.ID,
		"--author", "", "--listing", "", "--json")
	if code != 0 {
		t.Fatalf("update exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	var updated struct {
		Feed struct {
			AuthorHandle *string `json:"authorHandle"`
			ListingSlug  *string `json:"listingSlug"`
		} `json:"feed"`
	}
	if err := json.Unmarshal([]byte(stdout), &updated); err != nil {
		t.Fatalf("update response is not JSON: %v", err)
	}
	if updated.Feed.AuthorHandle != nil && *updated.Feed.AuthorHandle != "" {
		t.Errorf("authorHandle = %v, want null/empty after clear", updated.Feed.AuthorHandle)
	}
	if updated.Feed.ListingSlug != nil && *updated.Feed.ListingSlug != "" {
		t.Errorf("listingSlug = %v, want null/empty after clear", updated.Feed.ListingSlug)
	}
}

// Update with no field flags is a client-side error (nothing to send).
func TestFeedUpdateEmpty(t *testing.T) {
	adminEnv(t)
	// Unreachable server: validation must fire before any request.
	_, stderr, code := executeAgainst(t, "http://127.0.0.1:1",
		"feed", "update", "01FDOESNOTEXIST0000000000")
	if code != output.ExitError {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, output.ExitError, stderr)
	}
	if !strings.Contains(stderr, "nothing to update") {
		t.Errorf("stderr = %q, want \"nothing to update\"", stderr)
	}
}

// create with only required --label/--url must put the CLI defaults on the
// wire and omit enabled so the server defaults true.
func TestFeedCreateWireDefaults(t *testing.T) {
	adminEnv(t)
	srv, last := newCaptureServer(t)

	_, stderr, code := executeAgainst(t, srv.URL,
		"feed", "create",
		"--label", "Bare",
		"--url", "https://github.com/example/repo",
		"--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	var sent map[string]any
	if err := json.Unmarshal(last.body, &sent); err != nil {
		t.Fatalf("sent body is not JSON: %v", err)
	}
	if sent["kind"] != "github_release" {
		t.Errorf("kind = %v, want github_release", sent["kind"])
	}
	if sent["defaultCategory"] != "Releases" {
		t.Errorf("defaultCategory = %v, want Releases", sent["defaultCategory"])
	}
	if sent["autoPublish"] != false {
		t.Errorf("autoPublish = %v, want false", sent["autoPublish"])
	}
	tags, ok := sent["defaultTags"].([]any)
	if !ok {
		t.Errorf("defaultTags = %T %v, want a JSON array", sent["defaultTags"], sent["defaultTags"])
	} else if tags == nil {
		t.Error("defaultTags is null, want a present array (possibly empty)")
	}
	if _, present := sent["enabled"]; present {
		t.Errorf("enabled = %v, want key ABSENT unless --disabled", sent["enabled"])
	}
	if sent["label"] != "Bare" || sent["feedUrl"] != "https://github.com/example/repo" {
		t.Errorf("sent = %v, want label/url from flags", sent)
	}
}

// create → --disabled → --enabled re-enables the feed.
func TestFeedUpdateReenable(t *testing.T) {
	adminEnv(t)
	srv := newMockServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL,
		"feed", "create",
		"--label", "Claude Code",
		"--url", "https://github.com/anthropics/claude-code",
		"--listing", "claude-code",
		"--author", "anthropic",
		"--json")
	if code != 0 {
		t.Fatalf("create exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	var created struct {
		Feed struct {
			ID string `json:"id"`
		} `json:"feed"`
	}
	if err := json.Unmarshal([]byte(stdout), &created); err != nil {
		t.Fatalf("create response is not JSON: %v", err)
	}

	stdout, stderr, code = executeAgainst(t, srv.URL,
		"feed", "update", created.Feed.ID, "--disabled", "--json")
	if code != 0 {
		t.Fatalf("disable exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	var disabled struct {
		Feed struct {
			Enabled bool `json:"enabled"`
		} `json:"feed"`
	}
	if err := json.Unmarshal([]byte(stdout), &disabled); err != nil {
		t.Fatalf("disable response is not JSON: %v", err)
	}
	if disabled.Feed.Enabled {
		t.Fatal("feed still enabled after --disabled")
	}

	stdout, stderr, code = executeAgainst(t, srv.URL,
		"feed", "update", created.Feed.ID, "--enabled", "--json")
	if code != 0 {
		t.Fatalf("reenable exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	var reenabled struct {
		Feed struct {
			Enabled bool `json:"enabled"`
		} `json:"feed"`
	}
	if err := json.Unmarshal([]byte(stdout), &reenabled); err != nil {
		t.Fatalf("reenable response is not JSON: %v", err)
	}
	if !reenabled.Feed.Enabled {
		t.Error("feed.Enabled = false, want true after --enabled")
	}
}
