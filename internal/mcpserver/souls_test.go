package mcpserver

import (
	"path/filepath"
	"testing"
)

// safeInstallPath guards soul_install's caller-supplied paths: an MCP server
// has no TTY to confirm a surprising write, so a relative path may not
// resolve outside the user's home directory. An absolute path is explicit
// intent and passes through as given.
func TestSafeInstallPath(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "ada")
	cwdInHome := filepath.Join(home, "project")
	cwdOutside := filepath.Join(string(filepath.Separator), "tmp", "work")

	tests := []struct {
		name    string
		path    string
		cwd     string
		want    string
		wantErr bool
	}{
		{
			"absolute path is honored as given",
			filepath.Join(string(filepath.Separator), "anywhere", "SOUL.md"),
			cwdOutside,
			filepath.Join(string(filepath.Separator), "anywhere", "SOUL.md"),
			false,
		},
		{
			"relative path under home resolves against cwd",
			filepath.Join("notes", "SOUL.md"),
			cwdInHome,
			filepath.Join(cwdInHome, "notes", "SOUL.md"),
			false,
		},
		{
			"dot-dot staying inside home resolves",
			filepath.Join("..", "other", "SOUL.md"),
			cwdInHome,
			filepath.Join(home, "other", "SOUL.md"),
			false,
		},
		{
			"dot-dot escaping home is refused",
			filepath.Join("..", "..", "..", "etc", "SOUL.md"),
			cwdInHome,
			"",
			true,
		},
		{
			"relative path outside home is refused",
			"SOUL.md",
			cwdOutside,
			"",
			true,
		},
		{
			"home itself is not a valid escape prefix trick",
			filepath.Join("..", "ada-evil", "SOUL.md"),
			home, // resolves to /home/ada-evil — sibling, not inside home
			"",
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeInstallPath(tt.path, tt.cwd, home)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("safeInstallPath(%q) = %q, want an escape error", tt.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("safeInstallPath(%q) error: %v", tt.path, err)
			}
			if got != tt.want {
				t.Errorf("safeInstallPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
