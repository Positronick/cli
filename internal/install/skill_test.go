package install

import (
	"os"
	"path/filepath"
	"testing"
)

// SkillDir is the per-target skills directory convention table — user-level
// (home-anchored) by default, project-level (cwd-anchored) with --project.
// codex has no user dir of its own, so it shares ~/.agents/skills at
// user-level and ./.agents/skills at project-level, alongside openclaw's
// project-level dir (its own user dir stays ~/.openclaw/skills).
func TestSkillDir(t *testing.T) {
	cwd, home := "/work/project", "/home/ada"
	tests := []struct {
		target  string
		project bool
		want    string
	}{
		{"agents", false, "/home/ada/.agents/skills"},
		{"claude", false, "/home/ada/.claude/skills"},
		{"cursor", false, "/home/ada/.cursor/skills"},
		{"grok", false, "/home/ada/.grok/skills"},
		{"codex", false, "/home/ada/.agents/skills"},
		{"openclaw", false, "/home/ada/.openclaw/skills"},
		{"agents", true, "/work/project/.agents/skills"},
		{"claude", true, "/work/project/.claude/skills"},
		{"cursor", true, "/work/project/.cursor/skills"},
		{"grok", true, "/work/project/.grok/skills"},
		{"codex", true, "/work/project/.agents/skills"},
		{"openclaw", true, "/work/project/.agents/skills"},
	}
	for _, tt := range tests {
		name := tt.target
		if tt.project {
			name += "/project"
		}
		t.Run(name, func(t *testing.T) {
			got, err := SkillDir(tt.target, tt.project, cwd, home)
			if err != nil {
				t.Fatalf("SkillDir(%q, %v) error: %v", tt.target, tt.project, err)
			}
			if got != filepath.FromSlash(tt.want) {
				t.Errorf("SkillDir(%q, %v) = %q, want %q", tt.target, tt.project, got, tt.want)
			}
		})
	}
}

func TestSkillDirUnknownTarget(t *testing.T) {
	if _, err := SkillDir("emacs", false, "/cwd", "/home"); err == nil {
		t.Fatal("SkillDir must reject an unknown target")
	}
}

func TestSkillDirUnknownTargetProject(t *testing.T) {
	if _, err := SkillDir("emacs", true, "/cwd", "/home"); err == nil {
		t.Fatal("SkillDir must reject an unknown target at project level too")
	}
}

// SkillName parses the SKILL.md frontmatter `name:` field, the folder-name
// invariant the install command enforces against the listing slug.
func TestSkillName(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"plain value", "---\nname: superpowers\ndescription: x\n---\nbody", "superpowers"},
		{"quoted value", "---\nname: \"superpowers\"\ndescription: x\n---\nbody", "superpowers"},
		{"single-quoted value", "---\nname: 'superpowers'\n---\nbody", "superpowers"},
		{"extra whitespace", "---\nname:    superpowers  \n---\nbody", "superpowers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SkillName(tt.body)
			if err != nil {
				t.Fatalf("SkillName() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("SkillName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSkillNameMissingFrontmatter(t *testing.T) {
	if _, err := SkillName("# just a heading\nno frontmatter here"); err == nil {
		t.Fatal("SkillName must reject a body with no frontmatter block")
	}
}

func TestSkillNameMissingName(t *testing.T) {
	if _, err := SkillName("---\ndescription: x\n---\nbody"); err == nil {
		t.Fatal("SkillName must reject frontmatter with no name field")
	}
}

// InstallSkill mirrors Install's gate-before-fetch invariant: an existing
// destination is refused without touching Fetch at all.
func TestInstallSkillGateRunsBeforeFetch(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "superpowers", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("hand-edited"), 0o644); err != nil {
		t.Fatal(err)
	}

	fetchCalled := false
	_, err := Skill(SkillOptions{
		Dir:  dir,
		Slug: "superpowers",
		Fetch: func() (string, error) {
			fetchCalled = true
			return "---\nname: superpowers\n---\nbody", nil
		},
	})
	if err == nil {
		t.Fatal("InstallSkill must refuse to overwrite without --force")
	}
	if fetchCalled {
		t.Error("InstallSkill must gate before fetching — a refused install must not download")
	}
}

// A frontmatter name that doesn't match the slug fails loud and writes
// nothing — the install folder name (the slug) is the contract, not
// whatever name the asset happens to carry.
func TestInstallSkillNameMismatch(t *testing.T) {
	dir := t.TempDir()
	_, err := Skill(SkillOptions{
		Dir:  dir,
		Slug: "superpowers",
		Fetch: func() (string, error) {
			return "---\nname: not-superpowers\n---\nbody", nil
		},
	})
	if err == nil {
		t.Fatal("InstallSkill must reject a frontmatter name that doesn't match the slug")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "superpowers", "SKILL.md")); !os.IsNotExist(statErr) {
		t.Error("InstallSkill must not write a file when the name mismatches")
	}
}

// The happy path writes the body verbatim to <dir>/<slug>/SKILL.md.
func TestInstallSkillWritesVerbatim(t *testing.T) {
	dir := t.TempDir()
	body := "---\nname: superpowers\ndescription: x\n---\n# Superpowers\n"
	res, err := Skill(SkillOptions{
		Dir:  dir,
		Slug: "superpowers",
		Fetch: func() (string, error) {
			return body, nil
		},
	})
	if err != nil {
		t.Fatalf("Skill() error: %v", err)
	}
	wantPath := filepath.Join(dir, "superpowers", "SKILL.md")
	if res.Path != wantPath {
		t.Errorf("Path = %q, want %q", res.Path, wantPath)
	}
	if res.Name != "superpowers" {
		t.Errorf("Name = %q, want %q", res.Name, "superpowers")
	}
	if res.Bytes != len(body) {
		t.Errorf("Bytes = %d, want %d", res.Bytes, len(body))
	}
	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("written body = %q, want verbatim %q", got, body)
	}
}
