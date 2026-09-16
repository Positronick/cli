package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/positronick/cli/internal/output"
)

// SkillTargets are the supported `skill install` destinations, in the order
// they are documented. agents is the shared standard (Codex, Cursor, Grok
// Build and OpenClaw all read it); codex has no user-level directory of its
// own and shares agents'.
var SkillTargets = []string{"agents", "claude", "cursor", "grok", "codex", "openclaw"}

// SkillDir returns the conventional skills directory for a target: a
// user-level (home-anchored) directory by default, or a project-level
// (cwd-anchored) directory when project is true. codex has no directory of
// its own at either level — it shares agents' (openclaw shares it too at
// project level; its user-level directory stays its own).
func SkillDir(target string, project bool, cwd, home string) (string, error) {
	root := home
	if project {
		root = cwd
	}
	switch target {
	case "agents", "codex":
		return filepath.Join(root, ".agents", "skills"), nil
	case "openclaw":
		if project {
			return filepath.Join(root, ".agents", "skills"), nil
		}
		return filepath.Join(root, ".openclaw", "skills"), nil
	case "claude":
		return filepath.Join(root, ".claude", "skills"), nil
	case "cursor":
		return filepath.Join(root, ".cursor", "skills"), nil
	case "grok":
		return filepath.Join(root, ".grok", "skills"), nil
	default:
		return "", fmt.Errorf("unknown skill target %q (valid: %s)",
			target, strings.Join(SkillTargets, ", "))
	}
}

// SkillName parses the SKILL.md frontmatter `name:` field. body must start
// with a "---" frontmatter block; a missing block or a missing/empty name
// field is an error. No YAML library — this is a narrow, pure scan.
func SkillName(body string) (string, error) {
	if !strings.HasPrefix(body, "---\n") {
		return "", fmt.Errorf("SKILL.md has no frontmatter name")
	}
	end := strings.Index(body[4:], "\n---")
	if end == -1 {
		return "", fmt.Errorf("SKILL.md has no frontmatter name")
	}
	frontmatter := body[4 : 4+end]
	for _, line := range strings.Split(frontmatter, "\n") {
		value, ok := strings.CutPrefix(line, "name:")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if value == "" {
			continue
		}
		return value, nil
	}
	return "", fmt.Errorf("SKILL.md has no frontmatter name")
}

// SkillOptions configures one hosted-SKILL.md install.
type SkillOptions struct {
	// Dir is the destination's parent directory: either a target's
	// conventional skills directory (SkillDir) or an explicit --path
	// override. The install folder itself is always Slug.
	Dir string
	// Slug is the listing's slug: it names the install folder
	// (<Dir>/<Slug>/SKILL.md) and must equal the fetched body's
	// frontmatter name.
	Slug string
	// Force overwrites an existing file without asking.
	Force bool
	// Interactive enables the Confirm prompt for overwrites.
	Interactive bool
	// Confirm asks the user to approve overwriting the named file. Required
	// when Interactive and the destination exists.
	Confirm func(prompt string) (bool, error)
	// OverwriteHint names the flag suggested when refusing to overwrite
	// non-interactively; defaults to "--force".
	OverwriteHint string
	// Fetch returns the verbatim SKILL.md body. It is called only after the
	// overwrite gate passes: the server's .md endpoint bumps the public
	// download counter, and a refused install is not a download.
	Fetch func() (string, error)
}

// SkillResult reports where a skill install landed, the name its
// frontmatter carried, and how many bytes were written.
type SkillResult struct {
	Path  string
	Name  string
	Bytes int
}

// Skill resolves <Dir>/<Slug>/SKILL.md, gates overwrites (reusing the
// same gate Install uses), fetches the body, verifies its frontmatter name
// equals Slug, and writes it verbatim. The name check runs after the gate
// but the file is written only once both pass, so a mismatch — like a
// refused overwrite — leaves nothing on disk.
func Skill(opts SkillOptions) (*SkillResult, error) {
	if opts.Fetch == nil {
		return nil, output.Errorf("install: no body fetcher configured")
	}
	dest := filepath.Join(opts.Dir, opts.Slug, "SKILL.md")

	if err := gateOverwrite(dest, Options{
		Force:         opts.Force,
		Interactive:   opts.Interactive,
		Confirm:       opts.Confirm,
		OverwriteHint: opts.OverwriteHint,
	}); err != nil {
		return nil, err
	}

	body, err := opts.Fetch()
	if err != nil {
		return nil, err
	}
	name, err := SkillName(body)
	if err != nil {
		return nil, err
	}
	if name != opts.Slug {
		return nil, output.Errorf("skill %q: SKILL.md frontmatter name %q does not match the slug",
			opts.Slug, name)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", filepath.Dir(dest), err)
	}
	if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", dest, err)
	}

	return &SkillResult{Path: dest, Name: name, Bytes: len(body)}, nil
}
