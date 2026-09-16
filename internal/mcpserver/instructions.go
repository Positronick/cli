package mcpserver

// serverInstructions is the MCP initialize-result `instructions` field: the
// usage guidance every connected client hands its model, with no install
// step. Workflow altitude only — per-tool caveats live in the tool
// descriptions, which clients already surface, so restating one here
// charges its tokens twice on every session. The exceptions are
// soul_install's and skill_install's side effects, dangerous enough to pay
// for twice.
const serverInstructions = `positronick is the registry of agent capabilities on positronick.com.

Souls are installable SOUL.md personality files: soul_search → soul_show →
soul_install. The wider registry of verified tooling (harnesses, CLIs, MCP
servers, memory, agents, skills, plugins, loops, bots): listing_search → listing_show.
Skills with a hosted asset install with skill_install.

Search results are slim cards; always fetch the full record with the _show
tool before acting on an entry. soul_install and skill_install have side
effects — they count as a public download and never overwrites — so decide
from soul_show or listing_show first.
The companion CLI offers the same data (every command takes --json); run
"positronick agent-docs" for its manual.`

// SetupInstructions is the human help `mcp serve` prints — instead of
// starting the server — when stdin is a terminal: an interactive shell is
// never the MCP client, so hand the user ready-to-paste setup for the major
// clients. Changing this text changes a golden file.
func SetupInstructions() string {
	return `positronick mcp serve — run this binary as an MCP server

stdin is a terminal, so the stdio server was not started. Register the
command with your MCP client and let it launch the server:

Claude Code:

  claude mcp add positronick -- positronick mcp serve

Cursor — add to ~/.cursor/mcp.json:

  {
    "mcpServers": {
      "positronick": {
        "command": "positronick",
        "args": ["mcp", "serve"]
      }
    }
  }

Any other MCP client that speaks stdio can launch ` + "`positronick mcp serve`" + `
directly — JSON-RPC on stdin/stdout. Pass --stdio to force the server even
when stdin is a terminal.
`
}
