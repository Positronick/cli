package mcpserver

import (
	"strings"
	"testing"
)

// SetupInstructions is what a human sees when `mcp serve` runs on a terminal:
// it must hand them ready-to-paste setup for the major MCP clients instead of
// hijacking their terminal with JSON-RPC. Each block is load-bearing — agents
// and users copy these lines verbatim.
func TestSetupInstructionsCoverTheMajorClients(t *testing.T) {
	got := SetupInstructions()

	for _, want := range []string{
		// Claude Code one-liner.
		"claude mcp add positronick -- positronick mcp serve",
		// Cursor mcp.json snippet, at its documented location.
		"~/.cursor/mcp.json",
		`"mcpServers"`,
		`"command": "positronick"`,
		`"args": ["mcp", "serve"]`,
		// The escape hatch for generic stdio clients.
		"--stdio",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("SetupInstructions() missing %q:\n%s", want, got)
		}
	}

	if !strings.HasSuffix(got, "\n") {
		t.Error("SetupInstructions() must end with a newline (it is printed verbatim)")
	}
}
