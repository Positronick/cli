package install

import (
	"os"
	"path/filepath"
	"testing"
)

// mkdirs creates each named directory under base.
func mkdirs(t *testing.T, base string, names ...string) string {
	t.Helper()
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(base, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

// DetectHarness picks the install target from on-disk harness markers so that
// `positronick soul install` lands the SOUL.md where the user's actual tooling
// reads it — cwd beats home (a project-local harness wins over a global one),
// and the priority order within a directory is hermes, claude, cursor, openclaw.
func TestDetectHarness(t *testing.T) {
	tests := []struct {
		name string
		cwd  []string // marker dirs to create in cwd
		home []string // marker dirs to create in home
		want string
	}{
		{"nothing anywhere", nil, nil, ""},
		{"hermes in cwd", []string{".hermes"}, nil, "hermes"},
		{"claude in cwd", []string{".claude"}, nil, "claude"},
		{"cursor in cwd", []string{".cursor"}, nil, "cursor"},
		{"openclaw in cwd", []string{".openclaw"}, nil, "openclaw"},
		{"hermes in home only", nil, []string{".hermes"}, "hermes"},
		{"hermes beats claude in the same dir", []string{".claude", ".hermes"}, nil, "hermes"},
		{"claude beats cursor in the same dir", []string{".cursor", ".claude"}, nil, "claude"},
		{"cursor beats openclaw in the same dir", []string{".openclaw", ".cursor"}, nil, "cursor"},
		{"any cwd match beats any home match", []string{".openclaw"}, []string{".hermes"}, "openclaw"},
		{"home is the fallback", nil, []string{".cursor"}, "cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cwd := mkdirs(t, t.TempDir(), tt.cwd...)
			home := mkdirs(t, t.TempDir(), tt.home...)
			if got := DetectHarness(cwd, home); got != tt.want {
				t.Errorf("DetectHarness = %q, want %q", got, tt.want)
			}
		})
	}
}

// A marker that is a plain file, not a directory, must not count: harnesses
// create directories, and a stray file named .hermes is not an install target.
func TestDetectHarnessIgnoresPlainFiles(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".hermes"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DetectHarness(cwd, t.TempDir()); got != "" {
		t.Errorf("DetectHarness = %q, want \"\" for a non-directory marker", got)
	}
}

// Empty cwd/home values are skipped, never treated as the filesystem root.
func TestDetectHarnessEmptyPaths(t *testing.T) {
	if got := DetectHarness("", ""); got != "" {
		t.Errorf("DetectHarness(\"\", \"\") = %q, want \"\"", got)
	}
}
