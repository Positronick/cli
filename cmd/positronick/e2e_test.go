package main_test

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/mockapi"
)

// e2e smoke: build the real binary and drive it against the mock API via
// POSITRONICK_BASE_URL, asserting the things only a real process shows —
// actual exit codes, stream separation, and that --json stdout is pure JSON.

// buildBinary compiles cmd/positronick once per test binary run.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "positronick")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// run executes the built binary with POSITRONICK_BASE_URL pointing at baseURL
// and returns stdout, stderr and the real process exit code.
func run(t *testing.T, bin, baseURL string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runEnv(t, bin, baseURL, nil, args...)
}

// runEnv is run with extra environment entries (later entries win).
func runEnv(t *testing.T, bin, baseURL string, extraEnv []string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "POSITRONICK_BASE_URL="+baseURL)
	cmd.Env = append(cmd.Env, extraEnv...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running %v: %v", args, err)
		}
		exitCode = exitErr.ExitCode()
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func TestE2ESmoke(t *testing.T) {
	bin := buildBinary(t)
	srv := httptest.NewServer(mockapi.Handler())
	defer srv.Close()

	t.Run("search --json is pure JSON on stdout", func(t *testing.T) {
		stdout, stderr, code := run(t, bin, srv.URL, "soul", "search", "o", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty in JSON mode", stderr)
		}
		var result struct {
			Query string `json:"query"`
			Count int    `json:"count"`
			Souls []struct {
				Slug string `json:"slug"`
			} `json:"souls"`
		}
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("stdout is not pure JSON: %v (%q)", err, stdout)
		}
		if result.Query != "o" || result.Count != 3 || len(result.Souls) != 3 {
			t.Errorf("result = %+v, want query o with 3 souls", result)
		}
	})

	t.Run("show --json", func(t *testing.T) {
		stdout, _, code := run(t, bin, srv.URL, "soul", "show", "sherlock", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		var detail struct {
			Soul struct {
				Slug    string `json:"slug"`
				Content string `json:"content"`
			} `json:"soul"`
		}
		if err := json.Unmarshal([]byte(stdout), &detail); err != nil {
			t.Fatalf("stdout is not pure JSON: %v (%q)", err, stdout)
		}
		if detail.Soul.Slug != "sherlock" || detail.Soul.Content == "" {
			t.Errorf("detail = %+v, want sherlock with content", detail)
		}
	})

	t.Run("show --raw prints the body verbatim", func(t *testing.T) {
		stdout, _, code := run(t, bin, srv.URL, "soul", "show", "sherlock", "--raw")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		if stdout != mockapi.Souls[0].Content {
			t.Errorf("stdout = %q, want the verbatim SOUL.md body", stdout)
		}
	})

	t.Run("show on a typo exits 3 with the envelope on stderr", func(t *testing.T) {
		stdout, stderr, code := run(t, bin, srv.URL, "soul", "show", "sherloc", "--json")
		if code != 3 {
			t.Errorf("exit code = %d, want 3", code)
		}
		if stdout != "" {
			t.Errorf("stdout = %q, want empty on error", stdout)
		}
		want := `{"error":{"code":"not_found","message":"soul \"sherloc\" not found","hint":"did you mean \"sherlock\"? Run: positronick soul list"}}` + "\n"
		if stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
	})

	t.Run("network failure exits 1", func(t *testing.T) {
		dead := httptest.NewServer(nil)
		dead.Close() // closed immediately: every dial is refused
		_, stderr, code := run(t, bin, dead.URL, "soul", "list", "--json")
		if code != 1 {
			t.Errorf("exit code = %d, want 1 (stderr: %s)", code, stderr)
		}
		if !strings.HasPrefix(stderr, `{"error":{"code":"error",`) {
			t.Errorf("stderr = %q, want the generic JSON error envelope", stderr)
		}
	})

	t.Run("login --json streams NDJSON and stores credentials 0600", func(t *testing.T) {
		cfgDir := t.TempDir()
		env := []string{"POSITRONICK_CONFIG_DIR=" + cfgDir, "POSITRONICK_API_KEY="}

		stdout, stderr, code := runEnv(t, bin, srv.URL, env, "login", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty in JSON mode", stderr)
		}

		lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
		if len(lines) != 2 {
			t.Fatalf("stdout = %q, want exactly 2 NDJSON lines", stdout)
		}
		var pending struct {
			Status   string `json:"status"`
			UserCode string `json:"userCode"`
		}
		if err := json.Unmarshal([]byte(lines[0]), &pending); err != nil {
			t.Fatalf("line 1 is not valid JSON: %v (%q)", err, lines[0])
		}
		if pending.Status != "pending" || pending.UserCode != "TJSP-LLAV" {
			t.Errorf("pending line = %+v, want status pending with the dashed code", pending)
		}
		var done struct {
			Status string `json:"status"`
			User   struct {
				Email string `json:"email"`
			} `json:"user"`
		}
		if err := json.Unmarshal([]byte(lines[1]), &done); err != nil {
			t.Fatalf("line 2 is not valid JSON: %v (%q)", err, lines[1])
		}
		if done.Status != "authenticated" || done.User.Email != "ada@example.com" {
			t.Errorf("authenticated line = %+v, want the /api/me identity", done)
		}

		info, err := os.Stat(filepath.Join(cfgDir, "credentials.json"))
		if err != nil {
			t.Fatalf("credentials.json missing after login: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("credentials.json mode = %o, want 0600", perm)
		}

		// The stored session must now authenticate a live status check.
		stdout, _, code = runEnv(t, bin, srv.URL, env, "auth", "status", "--json")
		if code != 0 {
			t.Fatalf("auth status exit code = %d, want 0", code)
		}
		var status struct {
			Authenticated bool   `json:"authenticated"`
			Method        string `json:"method"`
		}
		if err := json.Unmarshal([]byte(stdout), &status); err != nil {
			t.Fatalf("auth status stdout is not pure JSON: %v (%q)", err, stdout)
		}
		if !status.Authenticated || status.Method != "device" {
			t.Errorf("status = %+v, want an authenticated device session", status)
		}
	})

	t.Run("completion bash exits 0", func(t *testing.T) {
		stdout, _, code := run(t, bin, srv.URL, "completion", "bash")
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
		if stdout == "" {
			t.Error("completion bash must print a script")
		}
	})
}
