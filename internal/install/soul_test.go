package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/output"
)

const testBody = "---\nname: Sherlock\n---\n\n# SOUL.md\n\nYou are Sherlock.\n"

// staticFetch returns body without touching the network — Install's Fetch
// seam stands in for the counter-bumping .md endpoint.
func staticFetch(body string) func() (string, error) {
	return func() (string, error) { return body, nil }
}

// TargetPath is the per-framework SOUL.md convention table — the paths agents
// and docs promise. hermes/claude/openclaw are home-anchored; cursor is
// project-local (its rules dir lives in the repo).
func TestTargetPath(t *testing.T) {
	cwd, home := "/work/project", "/home/ada"
	tests := []struct {
		target string
		want   string
	}{
		{"hermes", "/home/ada/.hermes/SOUL.md"},
		{"openclaw", "/home/ada/.openclaw/SOUL.md"},
		{"claude", "/home/ada/.claude/SOUL.md"},
		{"cursor", "/work/project/.cursor/rules/soul.mdc"},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got, err := TargetPath(tt.target, cwd, home)
			if err != nil {
				t.Fatalf("TargetPath(%q) error: %v", tt.target, err)
			}
			if got != filepath.FromSlash(tt.want) {
				t.Errorf("TargetPath(%q) = %q, want %q", tt.target, got, tt.want)
			}
		})
	}
}

func TestTargetPathUnknownTarget(t *testing.T) {
	if _, err := TargetPath("emacs", "/cwd", "/home"); err == nil {
		t.Fatal("TargetPath must reject an unknown target")
	}
}

// The install contract: the SOUL.md body is written verbatim — byte for byte
// what the server returned — and parent directories are created.
func TestInstallWritesVerbatim(t *testing.T) {
	home := t.TempDir()
	res, err := Install(Options{
		Target: "hermes",
		Home:   home,
		Fetch:  staticFetch(testBody),
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	wantPath := filepath.Join(home, ".hermes", "SOUL.md")
	if res.Path != wantPath {
		t.Errorf("Path = %q, want %q", res.Path, wantPath)
	}
	if res.Bytes != len(testBody) {
		t.Errorf("Bytes = %d, want %d", res.Bytes, len(testBody))
	}
	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != testBody {
		t.Errorf("written body = %q, want the verbatim body %q", got, testBody)
	}
}

// An explicit Path overrides the target convention entirely.
func TestInstallExplicitPath(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "deep", "nested", "SOUL.md")
	res, err := Install(Options{Path: dest, Fetch: staticFetch(testBody)})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Path != dest {
		t.Errorf("Path = %q, want %q", res.Path, dest)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != testBody {
		t.Errorf("written body = %q, want verbatim", got)
	}
}

// The cursor target wraps the body in mdc frontmatter so Cursor always
// applies the rule; the body itself stays verbatim below the wrapper.
func TestInstallCursorWrapsBody(t *testing.T) {
	cwd := t.TempDir()
	res, err := Install(Options{
		Target:   "cursor",
		Cwd:      cwd,
		SoulName: "Sherlock",
		Fetch:    staticFetch(testBody),
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := "---\ndescription: Sherlock\nalwaysApply: true\n---\n" + testBody
	got, err := os.ReadFile(filepath.Join(cwd, ".cursor", "rules", "soul.mdc"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("cursor content = %q, want wrapper + verbatim body %q", got, want)
	}
	if res.Bytes != len(want) {
		t.Errorf("Bytes = %d, want %d (wrapper included)", res.Bytes, len(want))
	}
}

// claude --link makes the installed soul actually load: Claude Code reads
// CLAUDE.md, so the @-import line must be appended — once, never duplicated,
// and without destroying existing content.
func TestInstallClaudeLink(t *testing.T) {
	claudeMd := func(home string) string { return filepath.Join(home, ".claude", "CLAUDE.md") }

	t.Run("creates CLAUDE.md when missing", func(t *testing.T) {
		home := t.TempDir()
		if _, err := Install(Options{Target: "claude", Home: home, Link: true,
			Fetch: staticFetch(testBody)}); err != nil {
			t.Fatalf("Install: %v", err)
		}
		got, err := os.ReadFile(claudeMd(home))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != claudeImportLine+"\n" {
			t.Errorf("CLAUDE.md = %q, want just the import line", got)
		}
	})

	t.Run("appends to existing content, adding the missing newline", func(t *testing.T) {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(claudeMd(home), []byte("# My rules"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Install(Options{Target: "claude", Home: home, Link: true, Force: true,
			Fetch: staticFetch(testBody)}); err != nil {
			t.Fatalf("Install: %v", err)
		}
		got, err := os.ReadFile(claudeMd(home))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "# My rules\n"+claudeImportLine+"\n" {
			t.Errorf("CLAUDE.md = %q, want existing content then the import line", got)
		}
	})

	t.Run("idempotent: a second install never duplicates the line", func(t *testing.T) {
		home := t.TempDir()
		for range 2 {
			if _, err := Install(Options{Target: "claude", Home: home, Link: true, Force: true,
				Fetch: staticFetch(testBody)}); err != nil {
				t.Fatalf("Install: %v", err)
			}
		}
		got, err := os.ReadFile(claudeMd(home))
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(got), claudeImportLine); n != 1 {
			t.Errorf("import line appears %d times, want exactly 1:\n%s", n, got)
		}
	})

	t.Run("no --link, no CLAUDE.md touch", func(t *testing.T) {
		home := t.TempDir()
		if _, err := Install(Options{Target: "claude", Home: home,
			Fetch: staticFetch(testBody)}); err != nil {
			t.Fatalf("Install: %v", err)
		}
		if _, err := os.Stat(claudeMd(home)); !os.IsNotExist(err) {
			t.Errorf("CLAUDE.md must not exist without Link, stat err = %v", err)
		}
	})
}

// The overwrite matrix protects a hand-edited SOUL.md: interactively the user
// decides; non-interactively only an explicit --force may clobber it. A
// refused or declined overwrite must never fetch the body — the .md fetch
// bumps the public download counter, and a failed install is not a download.
func TestInstallOverwrite(t *testing.T) {
	seed := func(t *testing.T) (home, dest string) {
		t.Helper()
		home = t.TempDir()
		dest = filepath.Join(home, ".hermes", "SOUL.md")
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, []byte("hand-edited"), 0o644); err != nil {
			t.Fatal(err)
		}
		return home, dest
	}
	countingFetch := func(calls *int) func() (string, error) {
		return func() (string, error) { *calls++; return testBody, nil }
	}

	t.Run("non-interactive without Force refuses, exit 1, hint --force", func(t *testing.T) {
		home, dest := seed(t)
		fetches := 0
		_, err := Install(Options{Target: "hermes", Home: home, Fetch: countingFetch(&fetches)})
		var coded *output.CodedError
		if !errors.As(err, &coded) || coded.Code != output.ExitError {
			t.Fatalf("err = %v, want CodedError exit %d", err, output.ExitError)
		}
		if !strings.Contains(coded.Hint, "--force") {
			t.Errorf("hint = %q, want it to name --force", coded.Hint)
		}
		if fetches != 0 {
			t.Errorf("fetch ran %d times on a refused overwrite, want 0", fetches)
		}
		if got, _ := os.ReadFile(dest); string(got) != "hand-edited" {
			t.Errorf("existing file = %q, must be untouched", got)
		}
	})

	t.Run("the refusal hint names the caller's flag", func(t *testing.T) {
		home, _ := seed(t)
		_, err := Install(Options{Target: "hermes", Home: home, OverwriteHint: "--yes",
			Fetch: staticFetch(testBody)})
		var coded *output.CodedError
		if !errors.As(err, &coded) || !strings.Contains(coded.Hint, "--yes") {
			t.Fatalf("err = %v, want a hint naming --yes", err)
		}
	})

	t.Run("Force overwrites", func(t *testing.T) {
		home, dest := seed(t)
		if _, err := Install(Options{Target: "hermes", Home: home, Force: true,
			Fetch: staticFetch(testBody)}); err != nil {
			t.Fatalf("Install: %v", err)
		}
		if got, _ := os.ReadFile(dest); string(got) != testBody {
			t.Errorf("file = %q, want the new body", got)
		}
	})

	t.Run("interactive yes overwrites", func(t *testing.T) {
		home, dest := seed(t)
		asked := ""
		_, err := Install(Options{Target: "hermes", Home: home, Interactive: true,
			Confirm: func(prompt string) (bool, error) { asked = prompt; return true, nil },
			Fetch:   staticFetch(testBody)})
		if err != nil {
			t.Fatalf("Install: %v", err)
		}
		if !strings.Contains(asked, dest) {
			t.Errorf("prompt = %q, want it to name the file at stake", asked)
		}
		if got, _ := os.ReadFile(dest); string(got) != testBody {
			t.Errorf("file = %q, want the new body", got)
		}
	})

	t.Run("interactive no cancels, exit 2, nothing fetched or written", func(t *testing.T) {
		home, dest := seed(t)
		fetches := 0
		_, err := Install(Options{Target: "hermes", Home: home, Interactive: true,
			Confirm: func(string) (bool, error) { return false, nil },
			Fetch:   countingFetch(&fetches)})
		var coded *output.CodedError
		if !errors.As(err, &coded) || coded.Code != output.ExitCancelled {
			t.Fatalf("err = %v, want CodedError exit %d", err, output.ExitCancelled)
		}
		if fetches != 0 {
			t.Errorf("fetch ran %d times on a declined overwrite, want 0", fetches)
		}
		if got, _ := os.ReadFile(dest); string(got) != "hand-edited" {
			t.Errorf("existing file = %q, must be untouched", got)
		}
	})

	t.Run("no existing file, no prompt", func(t *testing.T) {
		home := t.TempDir()
		_, err := Install(Options{Target: "hermes", Home: home, Interactive: true,
			Confirm: func(string) (bool, error) {
				t.Fatal("Confirm must not be called when nothing would be overwritten")
				return false, nil
			},
			Fetch: staticFetch(testBody)})
		if err != nil {
			t.Fatalf("Install: %v", err)
		}
	})
}

// A fetch failure surfaces verbatim and writes nothing.
func TestInstallFetchError(t *testing.T) {
	home := t.TempDir()
	boom := errors.New("boom")
	_, err := Install(Options{Target: "hermes", Home: home,
		Fetch: func() (string, error) { return "", boom }})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the fetch error", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".hermes", "SOUL.md")); !os.IsNotExist(statErr) {
		t.Error("nothing must be written when the fetch fails")
	}
}

func TestInstallValidation(t *testing.T) {
	if _, err := Install(Options{Target: "emacs", Home: t.TempDir(),
		Fetch: staticFetch(testBody)}); err == nil {
		t.Error("unknown target without Path must fail")
	}
	if _, err := Install(Options{Target: "hermes", Home: t.TempDir()}); err == nil {
		t.Error("a nil Fetch must fail loud, not panic or write an empty file")
	}
}
