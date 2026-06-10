package main_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/mockapi"
)

// e2e install smoke: the real binary against the mock API's install handler.
// The file landing on disk byte-identical to the fixture is the install
// contract — `positronick soul install` must be as lossless as
// `curl .../api/souls/<slug>.md > SOUL.md`.
func TestE2ESoulInstall(t *testing.T) {
	bin := buildBinary(t)
	srv := httptest.NewServer(mockapi.InstallHandler())
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "SOUL.md")
	stdout, stderr, code := run(t, bin, srv.URL,
		"soul", "install", "sherlock", "--path", dest, "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty in JSON mode", stderr)
	}

	var result struct {
		Installed struct {
			Slug  string `json:"slug"`
			Path  string `json:"path"`
			Bytes int    `json:"bytes"`
		} `json:"installed"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout is not pure JSON: %v (%q)", err, stdout)
	}
	if result.Installed.Slug != "sherlock" || result.Installed.Path != dest {
		t.Errorf("result = %+v, want sherlock at %s", result.Installed, dest)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("installed file missing: %v", err)
	}
	if string(got) != mockapi.Souls[0].Content {
		t.Errorf("installed file = %q, want it byte-identical to the fixture %q",
			got, mockapi.Souls[0].Content)
	}
	if result.Installed.Bytes != len(mockapi.Souls[0].Content) {
		t.Errorf("bytes = %d, want %d", result.Installed.Bytes, len(mockapi.Souls[0].Content))
	}

	// A second install without --force must refuse (real exit code 1) and
	// leave the file alone — and must not have re-bumped the counter.
	stdout, stderr, code = run(t, bin, srv.URL,
		"soul", "install", "sherlock", "--path", dest, "--json")
	if code != 1 {
		t.Errorf("overwrite exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on refusal", stdout)
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("stderr = %q, want the --force hint", stderr)
	}
}
