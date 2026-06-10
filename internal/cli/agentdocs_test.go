package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// agent-docs --json is how agents bootstrap themselves onto this CLI: the
// shape must parse, cover every visible command, and carry the exit-code map.
func TestAgentDocsJSONShape(t *testing.T) {
	stdout, _, code := executeAgainst(t, "", "agent-docs", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	var docs struct {
		Commands []struct {
			Path        string `json:"path"`
			Use         string `json:"use"`
			Description string `json:"description"`
			Flags       []struct {
				Name      string `json:"name"`
				Shorthand string `json:"shorthand"`
				Usage     string `json:"usage"`
				Default   string `json:"default"`
			} `json:"flags"`
		} `json:"commands"`
		ExitCodes map[string]string `json:"exitCodes"`
	}
	if err := json.Unmarshal([]byte(stdout), &docs); err != nil {
		t.Fatalf("agent-docs --json is not valid JSON: %v", err)
	}

	wantCodes := map[string]string{
		"0": "ok", "1": "error", "2": "cancelled", "3": "not found", "4": "auth required",
	}
	if len(docs.ExitCodes) != len(wantCodes) {
		t.Errorf("exitCodes = %v, want %v", docs.ExitCodes, wantCodes)
	}
	for k, v := range wantCodes {
		if docs.ExitCodes[k] != v {
			t.Errorf("exitCodes[%q] = %q, want %q", k, docs.ExitCodes[k], v)
		}
	}

	paths := map[string]bool{}
	for _, c := range docs.Commands {
		if c.Path == "" || c.Use == "" {
			t.Errorf("command %+v must have path and use", c)
		}
		paths[c.Path] = true
	}
	// Root + every noun-verb leaf must be documented; help must not.
	for _, want := range []string{
		"positronick",
		"positronick soul search",
		"positronick soul list",
		"positronick soul show",
		"positronick harness search",
		"positronick cli list",
		"positronick mcp show",
		"positronick agent search",
		"positronick skill list",
		"positronick plugin show",
		"positronick loop show",
		"positronick agent-docs",
		"positronick completion",
		"positronick version",
	} {
		if !paths[want] {
			t.Errorf("agent-docs is missing command %q", want)
		}
	}
	if paths["positronick help"] {
		t.Error("agent-docs must not document the help command")
	}

	// The root entry must carry the global flags agents script against.
	for _, c := range docs.Commands {
		if c.Path != "positronick" {
			continue
		}
		flagNames := map[string]string{}
		for _, f := range c.Flags {
			flagNames[f.Name] = f.Default
		}
		for name, def := range map[string]string{
			"json": "false", "base-url": "", "no-color": "false", "quiet": "false", "yes": "false",
		} {
			if got, ok := flagNames[name]; !ok || got != def {
				t.Errorf("root flag %q = (%q, %v), want default %q", name, got, ok, def)
			}
		}
		if _, ok := flagNames["help"]; ok {
			t.Error("the auto-generated help flag must not be documented")
		}
	}
}

// Human mode is an llms.txt-style markdown manual: H1, intro, exit codes,
// envelope shape, then one H2 per command.
func TestAgentDocsMarkdown(t *testing.T) {
	stdout, _, code := executeAgainst(t, "", "agent-docs")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, want := range []string{
		"# positronick — agent manual",
		"| 3 | not found |",
		`{"error":{"code":"<code>","message":"<message>","hint":"<optional hint>"}}`,
		"## positronick soul show",
		"## positronick loop search",
		"`--limit`",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("agent-docs output missing %q", want)
		}
	}
}
