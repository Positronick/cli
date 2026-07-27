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
// the feed's lastStatus becomes "ok".
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
