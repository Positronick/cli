package cli

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// -update regenerates the golden files from current output:
//
//	go test ./internal/cli -update
//
// Golden diffs are public contract changes (AGENTS.md): call them out in the
// PR description.
var update = flag.Bool("update", false, "rewrite golden files from current output")

// goldenCases is the pinned command surface. Every entry runs against the
// fixed mockapi dataset; stdout must match testdata/golden/<name> byte for
// byte. JSON cases pin the --json contract, .txt cases pin the human layout.
var goldenCases = []struct {
	golden string
	args   []string
}{
	// --json contract
	{"soul-search.json", []string{"soul", "search", "o", "--json"}},
	{"soul-search-downloads.json", []string{"soul", "search", "o", "--sort", "downloads", "--limit", "2", "--json"}},
	{"soul-list.json", []string{"soul", "list", "--json"}},
	{"soul-list-category.json", []string{"soul", "list", "--category", "technical", "--json"}},
	{"soul-list-newest.json", []string{"soul", "list", "--sort", "newest", "--json"}},
	{"soul-show.json", []string{"soul", "show", "sherlock", "--json"}},
	{"harness-list.json", []string{"harness", "list", "--json"}},
	{"cli-search.json", []string{"cli", "search", "git", "--json"}},
	{"mcp-list.json", []string{"mcp", "list", "--json"}},
	{"skill-list.json", []string{"skill", "list", "--json"}},
	{"plugin-list.json", []string{"plugin", "list", "--json"}}, // empty set: [] not null
	{"agent-search.json", []string{"agent", "search", "zzz", "--json"}},
	{"loop-list.json", []string{"loop", "list", "--json"}},
	{"loop-show.json", []string{"loop", "show", "pr-to-green", "--json"}},
	{"research.json", []string{"research", "--json"}},
	{"research-kind.json", []string{"research", "--kind", "release", "--json"}},
	{"research-since.json", []string{"research", "--since", "2026-06-01T09:00:00.000Z", "--json"}},
	{"blog-list.json", []string{"blog", "list", "--json"}},
	{"blog-show.json", []string{"blog", "show", "positronick-cli-v0-1-0", "--json"}},
	{"agent-docs.json", []string{"agent-docs", "--json"}},
	// human layout
	{"soul-list.txt", []string{"soul", "list"}},
	{"soul-show.txt", []string{"soul", "show", "sherlock"}},
	{"loop-show.txt", []string{"loop", "show", "pr-to-green"}},
	{"research.txt", []string{"research"}},
	{"blog-show.txt", []string{"blog", "show", "positronick-cli-v0-1-0"}},
	{"agent-docs.txt", []string{"agent-docs"}},
}

func TestGolden(t *testing.T) {
	srv := newMockServer(t)

	for _, tc := range goldenCases {
		t.Run(tc.golden, func(t *testing.T) {
			stdout, stderr, code := executeAgainst(t, srv.URL, tc.args...)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
			}

			path := filepath.Join("testdata", "golden", tc.golden)
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(stdout), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("missing golden file (run `go test ./internal/cli -update`): %v", err)
			}
			if stdout != string(want) {
				t.Errorf("output does not match %s — a contract change?\ngot:\n%s\nwant:\n%s",
					path, stdout, want)
			}
		})
	}
}
