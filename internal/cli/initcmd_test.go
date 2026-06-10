package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/mockapi"
	"github.com/positronick/cli/internal/output"
)

// init with --soul is the full non-interactive bootstrap: harness report,
// soul install, and the suggested next steps — one pinned JSON shape.
func TestInitJSON(t *testing.T) {
	home := isolateHome(t)
	srv, mdHits := newInstallServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL, "init", "--soul", "sherlock", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	assertGolden(t, "init.json", strings.ReplaceAll(stdout, home, mockHome))

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

// The human layout reports what happened and what to do next.
func TestInitHuman(t *testing.T) {
	home := isolateHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".hermes"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv, _ := newInstallServer(t)

	stdout, stderr, code := executeAgainst(t, srv.URL, "init", "--soul", "sherlock")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{
		"Detected harness: hermes\n",
		fmt.Sprintf("Installed Sherlock v1.2.0 → %s\n", filepath.Join(home, ".hermes", "SOUL.md")),
		"NEXT STEPS",
		"grafana-mcp",
		"superpowers",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want it to contain %q", stdout, want)
		}
	}
}

// Non-interactive init without --soul must not guess a personality for the
// machine: it fails with the flag named.
func TestInitRequiresSoulNonInteractive(t *testing.T) {
	isolateHome(t)
	// mockHost is unreachable: passing proves init failed before any fetch.
	stdout, stderr, code := executeAgainst(t, mockHost, "init", "--json")
	if code != output.ExitError {
		t.Errorf("exit code = %d, want %d", code, output.ExitError)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty on error", stdout)
	}
	if !strings.Contains(stderr, "--soul") {
		t.Errorf("stderr = %q, want the --soul hint", stderr)
	}
}

// init applies overwrite protection like soul install, with --yes as its
// consent flag.
func TestInitOverwrite(t *testing.T) {
	home := isolateHome(t)
	srv, _ := newInstallServer(t)
	dest := filepath.Join(home, ".hermes", "SOUL.md")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("hand-edited"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := executeAgainst(t, srv.URL, "init", "--soul", "sherlock", "--json")
	if code != output.ExitError {
		t.Errorf("exit code = %d, want %d", code, output.ExitError)
	}
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("stderr = %q, want the --yes hint", stderr)
	}

	if _, _, code = executeAgainst(t, srv.URL, "init", "--soul", "sherlock", "--yes"); code != 0 {
		t.Fatalf("--yes exit code = %d, want 0", code)
	}
	if got, _ := os.ReadFile(dest); string(got) != mockapi.Souls[0].Content {
		t.Errorf("body = %q, want the fresh install after --yes", got)
	}
}

// The interactive chooser: a numbered pick returns that soul's slug;
// nonsense and out-of-range numbers fail loud; EOF cancels — init must never
// guess a personality for the machine.
func TestChooseSoul(t *testing.T) {
	cards := []api.SoulCard{
		{Slug: "moriarty", Name: "Moriarty", Tagline: "adversarial"},
		{Slug: "sherlock", Name: "Sherlock", Tagline: "deductive"},
	}

	t.Run("valid pick", func(t *testing.T) {
		var errW bytes.Buffer
		got, err := chooseSoul(strings.NewReader("2\n"), &errW, cards)
		if err != nil {
			t.Fatalf("chooseSoul: %v", err)
		}
		if got != "sherlock" {
			t.Errorf("chose %q, want sherlock (pick 2)", got)
		}
		for _, want := range []string{"1. Moriarty", "2. Sherlock", "[1-2]"} {
			if !strings.Contains(errW.String(), want) {
				t.Errorf("prompt = %q, want it to contain %q", errW.String(), want)
			}
		}
	})

	t.Run("out of range", func(t *testing.T) {
		var errW bytes.Buffer
		if _, err := chooseSoul(strings.NewReader("9\n"), &errW, cards); err == nil {
			t.Error("an out-of-range pick must fail")
		}
	})

	t.Run("not a number", func(t *testing.T) {
		var errW bytes.Buffer
		if _, err := chooseSoul(strings.NewReader("sherlock\n"), &errW, cards); err == nil {
			t.Error("a non-numeric pick must fail")
		}
	})

	t.Run("EOF cancels with exit 2", func(t *testing.T) {
		var errW bytes.Buffer
		_, err := chooseSoul(strings.NewReader(""), &errW, cards)
		var coded *output.CodedError
		if !errors.As(err, &coded) || coded.Code != output.ExitCancelled {
			t.Errorf("err = %v, want CodedError exit %d", err, output.ExitCancelled)
		}
	})

	t.Run("empty gallery", func(t *testing.T) {
		var errW bytes.Buffer
		if _, err := chooseSoul(strings.NewReader("1\n"), &errW, nil); err == nil {
			t.Error("an empty gallery must fail, not panic")
		}
	})
}
