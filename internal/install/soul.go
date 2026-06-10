package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/positronick/cli/internal/output"
)

// Targets are the supported install targets, in detection-priority order.
var Targets = []string{"hermes", "claude", "cursor", "openclaw"}

// claudeImportLine is the line --link appends to ~/.claude/CLAUDE.md so
// Claude Code actually loads the installed soul.
const claudeImportLine = "@~/.claude/SOUL.md"

// TargetPath returns the conventional SOUL.md location for a target.
// hermes, claude and openclaw are home-anchored; cursor is project-local
// (its rules live inside the repository).
func TargetPath(target, cwd, home string) (string, error) {
	switch target {
	case "hermes":
		return filepath.Join(home, ".hermes", "SOUL.md"), nil
	case "openclaw":
		return filepath.Join(home, ".openclaw", "SOUL.md"), nil
	case "claude":
		return filepath.Join(home, ".claude", "SOUL.md"), nil
	case "cursor":
		return filepath.Join(cwd, ".cursor", "rules", "soul.mdc"), nil
	default:
		return "", fmt.Errorf("unknown install target %q (valid: %s)",
			target, strings.Join(Targets, ", "))
	}
}

// Options configures one SOUL.md install.
type Options struct {
	// Target is one of Targets. It picks the destination (when Path is
	// empty) and the formatting (cursor wraps the body in mdc frontmatter).
	// An empty Target with a Path writes the body verbatim to Path.
	Target string
	// Path, when set, overrides the target's conventional destination.
	Path string
	// SoulName fills the cursor frontmatter description.
	SoulName string
	// Cwd and Home anchor the conventional paths (injected for tests).
	Cwd, Home string
	// Force overwrites an existing file without asking.
	Force bool
	// Link, with Target "claude", appends the @-import line to
	// ~/.claude/CLAUDE.md when not already present.
	Link bool
	// Interactive enables the Confirm prompt for overwrites.
	Interactive bool
	// Confirm asks the user to approve overwriting the named file. Required
	// when Interactive and the destination exists.
	Confirm func(prompt string) (bool, error)
	// OverwriteHint names the flag suggested when refusing to overwrite
	// non-interactively; defaults to "--force".
	OverwriteHint string
	// Fetch returns the verbatim SOUL.md body. It is called only after the
	// overwrite gate passes: the server's .md endpoint bumps the public
	// download counter, and a refused install is not a download.
	Fetch func() (string, error)
}

// Result reports where an install landed and how many bytes were written.
type Result struct {
	Path  string
	Bytes int
}

// Install resolves the destination, gates overwrites (prompt when
// Interactive, Force otherwise), fetches the body, writes it (creating
// parent directories), and runs the claude CLAUDE.md link when asked.
func Install(opts Options) (*Result, error) {
	if opts.Fetch == nil {
		return nil, output.Errorf("install: no body fetcher configured")
	}
	dest := opts.Path
	if dest == "" {
		var err error
		if dest, err = TargetPath(opts.Target, opts.Cwd, opts.Home); err != nil {
			return nil, output.Errorf("%s", err)
		}
	} else if opts.Target != "" {
		// An explicit Target alongside Path still validates the target name
		// (it decides formatting), without using its conventional path.
		if _, err := TargetPath(opts.Target, opts.Cwd, opts.Home); err != nil {
			return nil, output.Errorf("%s", err)
		}
	}

	if err := gateOverwrite(dest, opts); err != nil {
		return nil, err
	}

	body, err := opts.Fetch()
	if err != nil {
		return nil, err
	}
	content := body
	if opts.Target == "cursor" {
		content = fmt.Sprintf("---\ndescription: %s\nalwaysApply: true\n---\n", opts.SoulName) + body
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", filepath.Dir(dest), err)
	}
	if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", dest, err)
	}

	if opts.Target == "claude" && opts.Link {
		if err := linkClaude(opts.Home); err != nil {
			return nil, err
		}
	}
	return &Result{Path: dest, Bytes: len(content)}, nil
}

// gateOverwrite decides whether writing to dest may proceed: a missing file
// or Force always passes; otherwise an interactive run asks Confirm (a "no"
// cancels, exit 2) and a non-interactive run refuses with a hint naming the
// overwrite flag (exit 1).
func gateOverwrite(dest string, opts Options) error {
	if opts.Force {
		return nil
	}
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		return nil
	}
	if opts.Interactive && opts.Confirm != nil {
		ok, err := opts.Confirm(fmt.Sprintf("%s already exists — overwrite?", dest))
		if err != nil {
			return err
		}
		if !ok {
			return output.CancelledError("install cancelled")
		}
		return nil
	}
	hint := opts.OverwriteHint
	if hint == "" {
		hint = "--force"
	}
	return output.ErrorWithHint(
		fmt.Sprintf("%s already exists", dest),
		fmt.Sprintf("re-run with %s to overwrite", hint))
}

// linkClaude appends claudeImportLine to ~/.claude/CLAUDE.md when the line
// is not already present, creating the file if needed and preserving
// existing content (a missing trailing newline is added before appending).
func linkClaude(home string) error {
	path := filepath.Join(home, ".claude", "CLAUDE.md")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	for line := range strings.Lines(string(existing)) {
		if strings.TrimSpace(line) == claudeImportLine {
			return nil
		}
	}
	content := string(existing)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += claudeImportLine + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
