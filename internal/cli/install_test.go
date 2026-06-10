package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/mockapi"
	"github.com/positronick/cli/internal/output"
)

// mockHome is the stable stand-in for the per-test fake home directory in
// golden files.
const mockHome = "/home/mock"

// isolateHome gives the test a fresh fake home (so harness detection and
// target paths never touch the developer's real dotfiles) on top of the
// usual auth isolation.
func isolateHome(t *testing.T) string {
	t.Helper()
	isolateAuth(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// newInstallServer serves the mock API including the counter-bumping .md
// endpoint, counting its hits: tests assert the counter is bumped exactly
// once per successful install and never on a refused one.
func newInstallServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var mdHits atomic.Int32
	inner := mockapi.InstallHandler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".md") {
			mdHits.Add(1)
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &mdHits
}

// The default install: no flags, no detected harness → hermes, the verbatim
// body at ~/.hermes/SOUL.md, and exactly one download-counter bump.
func TestSoulInstallDefaultJSON(t *testing.T) {
	home := isolateHome(t)
	srv, mdHits := newInstallServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL, "soul", "install", "sherlock", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty in JSON mode", stderr)
	}
	assertGolden(t, "soul-install.json", strings.ReplaceAll(stdout, home, mockHome))

	got, err := os.ReadFile(filepath.Join(home, ".hermes", "SOUL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != mockapi.Souls[0].Content {
		t.Errorf("installed body = %q, want the verbatim fixture body", got)
	}
	if n := mdHits.Load(); n != 1 {
		t.Errorf(".md endpoint hit %d times, want exactly 1", n)
	}
}

func TestSoulInstallHuman(t *testing.T) {
	home := isolateHome(t)
	srv, _ := newInstallServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL, "soul", "install", "sherlock")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := fmt.Sprintf("Installed Sherlock v1.2.0 → %s\n",
		filepath.Join(home, ".hermes", "SOUL.md"))
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// The detected harness picks the target: a .claude marker in the fake home
// routes the install to ~/.claude/SOUL.md without any flag.
func TestSoulInstallDetectsHarness(t *testing.T) {
	home := isolateHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv, _ := newInstallServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL, "soul", "install", "sherlock", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, `"target": "claude"`) {
		t.Errorf("stdout = %q, want the detected claude target", stdout)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "SOUL.md")); err != nil {
		t.Errorf("SOUL.md missing at the claude path: %v", err)
	}
}

// --path is the exact-destination escape hatch: verbatim bytes, no wrapper,
// and the JSON target reports "path" since no harness semantics applied.
func TestSoulInstallExplicitPath(t *testing.T) {
	isolateHome(t)
	srv, _ := newInstallServer(t)
	dest := filepath.Join(t.TempDir(), "SOUL.md")

	stdout, stderr, code := executeAgainst(t, srv.URL,
		"soul", "install", "sherlock", "--path", dest, "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, `"target": "path"`) {
		t.Errorf("stdout = %q, want target \"path\" for a bare --path install", stdout)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != mockapi.Souls[0].Content {
		t.Errorf("body = %q, want verbatim", got)
	}
}

// --target cursor writes the project-local rule, wrapped.
func TestSoulInstallCursorTarget(t *testing.T) {
	isolateHome(t)
	t.Chdir(t.TempDir())
	srv, _ := newInstallServer(t)

	_, stderr, code := executeAgainst(t, srv.URL,
		"soul", "install", "sherlock", "--target", "cursor", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(cwd, ".cursor", "rules", "soul.mdc"))
	if err != nil {
		t.Fatal(err)
	}
	want := "---\ndescription: Sherlock\nalwaysApply: true\n---\n" + mockapi.Souls[0].Content
	if string(got) != want {
		t.Errorf("cursor rule = %q, want frontmatter + verbatim body", got)
	}
}

// Non-interactive overwrite is refused without --force — and the refused
// attempt must not bump the download counter.
func TestSoulInstallOverwriteNonInteractive(t *testing.T) {
	home := isolateHome(t)
	srv, mdHits := newInstallServer(t)
	dest := filepath.Join(home, ".hermes", "SOUL.md")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("hand-edited"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := executeAgainst(t, srv.URL, "soul", "install", "sherlock", "--json")
	if code != output.ExitError {
		t.Fatalf("exit code = %d, want %d", code, output.ExitError)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on error", stdout)
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("stderr = %q, want the --force hint", stderr)
	}
	if n := mdHits.Load(); n != 0 {
		t.Errorf(".md endpoint hit %d times on a refused install, want 0", n)
	}

	if _, _, code = executeAgainst(t, srv.URL, "soul", "install", "sherlock", "--force"); code != 0 {
		t.Fatalf("--force exit code = %d, want 0", code)
	}
	if got, _ := os.ReadFile(dest); string(got) != mockapi.Souls[0].Content {
		t.Errorf("body = %q, want the fresh install after --force", got)
	}
}

// --link is claude-specific and incompatible with --path; both misuses fail
// before any network call.
func TestSoulInstallLinkValidation(t *testing.T) {
	isolateHome(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"link with hermes", []string{"soul", "install", "sherlock", "--link"}, "--target claude"},
		{"link with path", []string{"soul", "install", "sherlock", "--link",
			"--target", "claude", "--path", "/tmp/x"}, "--path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// mockHost is unreachable: passing proves no network call happened.
			_, stderr, code := executeAgainst(t, mockHost, tt.args...)
			if code != output.ExitError {
				t.Errorf("exit code = %d, want %d", code, output.ExitError)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want it to mention %q", stderr, tt.want)
			}
		})
	}
}

// An unknown --target fails loud, before any network call.
func TestSoulInstallUnknownTarget(t *testing.T) {
	isolateHome(t)
	_, stderr, code := executeAgainst(t, mockHost, "soul", "install", "sherlock", "--target", "emacs")
	if code != output.ExitError {
		t.Errorf("exit code = %d, want %d", code, output.ExitError)
	}
	if !strings.Contains(stderr, "emacs") {
		t.Errorf("stderr = %q, want the bad target named", stderr)
	}
}

// A typo'd slug keeps the read commands' did-you-mean behavior on install.
func TestSoulInstallNotFound(t *testing.T) {
	isolateHome(t)
	srv, mdHits := newInstallServer(t)
	_, stderr, code := executeAgainst(t, srv.URL, "soul", "install", "sherloc", "--json")
	if code != output.ExitNotFound {
		t.Errorf("exit code = %d, want %d", code, output.ExitNotFound)
	}
	if !strings.Contains(stderr, `did you mean \"sherlock\"`) {
		t.Errorf("stderr = %q, want the did-you-mean hint", stderr)
	}
	if n := mdHits.Load(); n != 0 {
		t.Errorf(".md endpoint hit %d times for a missing soul, want 0", n)
	}
}

// loop install prints the registry's kickoff prompt.
func TestLoopInstall(t *testing.T) {
	isolateHome(t)
	srv := newMockServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL, "loop", "install", "pr-to-green", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	assertGolden(t, "loop-install.json", stdout)

	stdout, _, code = executeAgainst(t, srv.URL, "loop", "install", "pr-to-green")
	if code != 0 {
		t.Fatalf("human exit code = %d, want 0", code)
	}
	if !strings.HasPrefix(stdout, "KICKOFF\n") {
		t.Errorf("stdout = %q, want the KICKOFF heading first", stdout)
	}
	if !strings.Contains(stdout, "Run the pr-to-green loop") {
		t.Errorf("stdout = %q, want the kickoff prompt", stdout)
	}
}

// Cross-type installs get the existing cross-type hint, pointed at install.
func TestInstallWrongTypeHint(t *testing.T) {
	isolateHome(t)
	srv := newMockServer(t)
	_, stderr, code := executeAgainst(t, srv.URL, "loop", "install", "claude-code", "--json")
	if code != output.ExitNotFound {
		t.Errorf("exit code = %d, want %d", code, output.ExitNotFound)
	}
	want := "positronick harness install claude-code"
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want the hint %q", stderr, want)
	}
}

// A loop without a kickoff still installs: the prompt is generated from the
// recipe fields that exist.
func TestGeneratedKickoff(t *testing.T) {
	l := &api.Listing{Name: "PR to Green"}
	full := api.LoopData{
		Goal:          "CI green",
		CheckCommand:  "gh pr checks",
		ExitCondition: "all checks pass",
		MaxIterations: 5,
	}
	got := generatedKickoff(l, full)
	for _, want := range []string{"PR to Green", "Goal: CI green", "gh pr checks",
		"Stop when: all checks pass", "5 iterations"} {
		if !strings.Contains(got, want) {
			t.Errorf("kickoff = %q, want it to contain %q", got, want)
		}
	}

	if got := generatedKickoff(l, api.LoopData{}); !strings.Contains(got, "PR to Green") {
		t.Errorf("kickoff for an empty recipe = %q, want at least the loop named", got)
	}
}

// Default listing install prints, never executes: the official command when
// registered, the source URL when not.
func TestListingInstallPrints(t *testing.T) {
	isolateHome(t)
	srv := newMockServer(t)

	t.Run("installCmd json", func(t *testing.T) {
		stdout, stderr, code := executeAgainst(t, srv.URL, "harness", "install", "claude-code", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "harness-install.json", stdout)
	})

	t.Run("installCmd human is pipe-clean", func(t *testing.T) {
		stdout, stderr, code := executeAgainst(t, srv.URL, "harness", "install", "claude-code")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		if stdout != "npm install -g @anthropic-ai/claude-code\n" {
			t.Errorf("stdout = %q, want exactly the install command", stdout)
		}
		if !strings.Contains(stderr, "--run") {
			t.Errorf("stderr = %q, want the --run hint", stderr)
		}
	})

	t.Run("source URL fallback", func(t *testing.T) {
		stdout, stderr, code := executeAgainst(t, srv.URL, "mcp", "install", "grafana-mcp", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		assertGolden(t, "mcp-install.json", stdout)

		stdout, stderr, code = executeAgainst(t, srv.URL, "mcp", "install", "grafana-mcp")
		if code != 0 {
			t.Fatalf("human exit code = %d, want 0", code)
		}
		if stdout != "https://example.com/grafana-mcp\n" {
			t.Errorf("stdout = %q, want the source URL", stdout)
		}
		if !strings.Contains(stderr, "no official install command") {
			t.Errorf("stderr = %q, want the fallback explained", stderr)
		}
	})
}

// stubRunShell replaces the shell runner for one test, recording the command.
func stubRunShell(t *testing.T, exitCode int, out string) *string {
	t.Helper()
	var gotCmd string
	orig := runShell
	runShell = func(_ context.Context, command string, stdout, _ io.Writer) (int, error) {
		gotCmd = command
		_, _ = io.WriteString(stdout, out)
		return exitCode, nil
	}
	t.Cleanup(func() { runShell = orig })
	return &gotCmd
}

// --run is double-gated for agents: non-interactive runs without --yes are
// refused with the exact command in the hint, and nothing executes.
func TestListingInstallRunRequiresYes(t *testing.T) {
	isolateHome(t)
	srv := newMockServer(t)
	gotCmd := stubRunShell(t, 0, "")

	stdout, stderr, code := executeAgainst(t, srv.URL, "harness", "install", "claude-code", "--run", "--json")
	if code != output.ExitError {
		t.Errorf("exit code = %d, want %d", code, output.ExitError)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on refusal", stdout)
	}
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("stderr = %q, want the --yes hint", stderr)
	}
	if *gotCmd != "" {
		t.Errorf("command %q was executed despite the refusal", *gotCmd)
	}
}

// --run --yes executes via sh -c and reports the exit code in JSON; a
// non-zero command exit becomes the process exit code.
func TestListingInstallRunExecutes(t *testing.T) {
	isolateHome(t)
	srv := newMockServer(t)

	t.Run("success", func(t *testing.T) {
		gotCmd := stubRunShell(t, 0, "installer output\n")
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"harness", "install", "claude-code", "--run", "--yes", "--json")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		if *gotCmd != "npm install -g @anthropic-ai/claude-code" {
			t.Errorf("executed %q, want the listing's install command", *gotCmd)
		}
		// The child's stdout was streamed to stderr so stdout stays pure JSON.
		if !strings.Contains(stderr, "installer output") {
			t.Errorf("stderr = %q, want the streamed command output", stderr)
		}
		assertGolden(t, "harness-install-run.json", stdout)
	})

	t.Run("command failure propagates its exit code", func(t *testing.T) {
		stubRunShell(t, 3, "")
		stdout, stderr, code := executeAgainst(t, srv.URL,
			"harness", "install", "claude-code", "--run", "--yes", "--json")
		if code != 3 {
			t.Errorf("exit code = %d, want the command's 3", code)
		}
		if !strings.Contains(stdout, `"exitCode": 3`) {
			t.Errorf("stdout = %q, want the result with exitCode 3", stdout)
		}
		if !strings.Contains(stderr, "exited with code 3") {
			t.Errorf("stderr = %q, want the failure envelope", stderr)
		}
	})

	t.Run("no official command refuses to run", func(t *testing.T) {
		stubRunShell(t, 0, "")
		_, stderr, code := executeAgainst(t, srv.URL,
			"mcp", "install", "grafana-mcp", "--run", "--yes", "--json")
		if code != output.ExitError {
			t.Errorf("exit code = %d, want %d", code, output.ExitError)
		}
		if !strings.Contains(stderr, "no official install command") {
			t.Errorf("stderr = %q, want the missing-command error", stderr)
		}
	})
}

// The real runner streams both channels and reports the shell's exit code.
func TestRunShellReal(t *testing.T) {
	var out, errBuf bytes.Buffer
	code, err := runShell(context.Background(), "echo to-stdout; echo to-stderr 1>&2; exit 3",
		&out, &errBuf)
	if err != nil {
		t.Fatalf("runShell: %v", err)
	}
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
	if out.String() != "to-stdout\n" {
		t.Errorf("stdout = %q, want %q", out.String(), "to-stdout\n")
	}
	if errBuf.String() != "to-stderr\n" {
		t.Errorf("stderr = %q, want %q", errBuf.String(), "to-stderr\n")
	}
}

// On a claude machine with the claude binary present, mcp install suggests
// the ready `claude mcp add` line — and only then: no harness or no binary
// means no suggestion, and it never appears for listings without a command.
func TestSuggestClaudeMCPAdd(t *testing.T) {
	cmdStr := "npx grafana-mcp"
	listing := &api.Listing{Slug: "grafana-mcp", Type: "mcp", InstallCmd: &cmdStr}
	printer := func(buf *bytes.Buffer) *output.Printer {
		return &output.Printer{Out: io.Discard, Err: buf, Mode: output.Mode{}}
	}
	stubLookPath := func(t *testing.T, found bool) {
		t.Helper()
		orig := lookPath
		lookPath = func(string) (string, error) {
			if found {
				return "/usr/local/bin/claude", nil
			}
			return "", os.ErrNotExist
		}
		t.Cleanup(func() { lookPath = orig })
	}

	t.Run("claude harness with binary suggests", func(t *testing.T) {
		home := isolateHome(t)
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		stubLookPath(t, true)
		var buf bytes.Buffer
		suggestClaudeMCPAdd(printer(&buf), listing)
		want := "claude mcp add grafana-mcp -- npx grafana-mcp"
		if !strings.Contains(buf.String(), want) {
			t.Errorf("stderr = %q, want it to contain %q", buf.String(), want)
		}
	})

	t.Run("no claude harness stays silent", func(t *testing.T) {
		isolateHome(t)
		stubLookPath(t, true)
		var buf bytes.Buffer
		suggestClaudeMCPAdd(printer(&buf), listing)
		if buf.String() != "" {
			t.Errorf("stderr = %q, want silence without the claude harness", buf.String())
		}
	})

	t.Run("claude harness without the binary stays silent", func(t *testing.T) {
		home := isolateHome(t)
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		stubLookPath(t, false)
		var buf bytes.Buffer
		suggestClaudeMCPAdd(printer(&buf), listing)
		if buf.String() != "" {
			t.Errorf("stderr = %q, want silence without the claude binary", buf.String())
		}
	})

	t.Run("no install command stays silent", func(t *testing.T) {
		home := isolateHome(t)
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		stubLookPath(t, true)
		var buf bytes.Buffer
		suggestClaudeMCPAdd(printer(&buf), &api.Listing{Slug: "x", Type: "mcp"})
		if buf.String() != "" {
			t.Errorf("stderr = %q, want silence without an install command", buf.String())
		}
	})
}
