package main

// skills/positronick/SKILL.md is published on the positronick.com registry
// as the `positronick` skill listing, so the enumerations it hardcodes are a
// contract pinned against their Go sources here. Matching the exact joined
// list — not per-item substrings — means an entry added, removed, renamed or
// reordered in the Go slice fails CI until the published skill is updated.

import (
	"os"
	"strings"
	"testing"

	"github.com/positronick/cli/internal/api"
	"github.com/positronick/cli/internal/install"
)

func TestSkillPinsTargetAndTypeLists(t *testing.T) {
	body, err := os.ReadFile("../../skills/positronick/SKILL.md")
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	// Markdown wraps lists across lines; match against unwrapped prose.
	got := strings.ReplaceAll(string(body), "\n", " ")

	for name, list := range map[string][]string{
		"install targets": install.Targets,
		"listing types":   api.ListingTypes,
	} {
		want := "`" + strings.Join(list, "`, `") + "`"
		if !strings.Contains(got, want) {
			t.Errorf("SKILL.md does not contain the exact %s list %s — update the published skill", name, want)
		}
	}
}
