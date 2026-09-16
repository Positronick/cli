package main

// This repo's root doubles as a Claude Code plugin (.claude-plugin/plugin.json,
// .mcp.json, .claude-plugin/marketplace.json) so `/plugin marketplace add
// Positronick/cli` + `/plugin install positronick@positronick` work without a
// separate plugin repo. This test pins the three manifests against each other
// and against skills/positronick/SKILL.md (the plugin's bundled skill) so a
// rename in one place fails CI instead of breaking the Claude Code install.

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
)

// pluginNamePattern is the name format documented for Claude Code plugins.
var pluginNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

type pluginManifest struct {
	Name string `json:"name"`
}

type mcpManifest struct {
	MCPServers map[string]struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	} `json:"mcpServers"`
}

type marketplaceManifest struct {
	Plugins []struct {
		Name string `json:"name"`
	} `json:"plugins"`
}

func TestClaudePluginManifestsMatch(t *testing.T) {
	pluginRaw, err := os.ReadFile("../../.claude-plugin/plugin.json")
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	var plugin pluginManifest
	if err := json.Unmarshal(pluginRaw, &plugin); err != nil {
		t.Fatalf("parse plugin.json: %v", err)
	}
	if plugin.Name != "positronick" {
		t.Errorf("plugin.json name = %q, want %q", plugin.Name, "positronick")
	}
	if !pluginNamePattern.MatchString(plugin.Name) {
		t.Errorf("plugin.json name %q does not match documented pattern %s", plugin.Name, pluginNamePattern.String())
	}

	marketplaceRaw, err := os.ReadFile("../../.claude-plugin/marketplace.json")
	if err != nil {
		t.Fatalf("read marketplace.json: %v", err)
	}
	var marketplace marketplaceManifest
	if err := json.Unmarshal(marketplaceRaw, &marketplace); err != nil {
		t.Fatalf("parse marketplace.json: %v", err)
	}
	if len(marketplace.Plugins) != 1 {
		t.Fatalf("marketplace.json plugins = %d entries, want 1", len(marketplace.Plugins))
	}
	if marketplace.Plugins[0].Name != plugin.Name {
		t.Errorf("marketplace.json plugin name = %q, want %q (plugin.json)", marketplace.Plugins[0].Name, plugin.Name)
	}

	mcpRaw, err := os.ReadFile("../../.mcp.json")
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	var mcpConfig mcpManifest
	if err := json.Unmarshal(mcpRaw, &mcpConfig); err != nil {
		t.Fatalf("parse .mcp.json: %v", err)
	}
	server, ok := mcpConfig.MCPServers["positronick"]
	if !ok {
		t.Fatalf(".mcp.json missing mcpServers.positronick")
	}
	if server.Command != "positronick" {
		t.Errorf(".mcp.json mcpServers.positronick.command = %q, want %q", server.Command, "positronick")
	}
	wantArgs := []string{"mcp", "serve"}
	if len(server.Args) != len(wantArgs) {
		t.Errorf(".mcp.json mcpServers.positronick.args = %v, want %v", server.Args, wantArgs)
	} else {
		for i, a := range wantArgs {
			if server.Args[i] != a {
				t.Errorf(".mcp.json mcpServers.positronick.args = %v, want %v", server.Args, wantArgs)
				break
			}
		}
	}

	if _, err := os.Stat("../../skills/positronick/SKILL.md"); err != nil {
		t.Errorf("skills/positronick/SKILL.md (the plugin's bundled skill) must exist: %v", err)
	}
}
