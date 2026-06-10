// Package install owns the SOUL.md install conventions: harness detection,
// the per-framework target table, the overwrite gate, and the install
// receipt written by install.sh. Paths are injected so everything is testable
// without touching the real home directory.
package install

import (
	"os"
	"path/filepath"
)

// harnessMarkers maps each install target to the directory that betrays the
// harness's presence, in priority order: hermes first (the issue-#9 default),
// then claude, cursor, openclaw.
var harnessMarkers = []struct{ dir, target string }{
	{".hermes", "hermes"},
	{".claude", "claude"},
	{".cursor", "cursor"},
	{".openclaw", "openclaw"},
}

// DetectHarness returns the install target for the first harness marker
// directory found — all markers are checked in cwd first (a project-local
// harness wins), then in home — or "" when none exists. Only directories
// count; an empty cwd/home is skipped.
func DetectHarness(cwd, home string) string {
	for _, base := range []string{cwd, home} {
		if base == "" {
			continue
		}
		for _, m := range harnessMarkers {
			if info, err := os.Stat(filepath.Join(base, m.dir)); err == nil && info.IsDir() {
				return m.target
			}
		}
	}
	return ""
}
