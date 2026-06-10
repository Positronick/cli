# Positronick CLI

`positronick` discovers and installs agent capabilities — souls, harnesses, CLIs, MCP servers, agents, skills, plugins, and loops — from [positronick.com](https://positronick.com).

## Install

```sh
curl -fsSL https://positronick.com/install.sh | sh
```

> The install script goes live with v0.1.0.

Or with Go:

```sh
go install github.com/positronick/cli/cmd/positronick@latest
```

## Usage

| Command | Description | Status |
| --- | --- | --- |
| `positronick soul search\|show\|list\|install` | Find and install `SOUL.md` personality files | (coming in v0.1.0) |
| `positronick harness search\|show\|list\|install` | Agent harnesses | (coming in v0.1.0) |
| `positronick cli search\|show\|list\|install` | CLI tools | (coming in v0.1.0) |
| `positronick mcp search\|show\|list\|install` | MCP servers | (coming in v0.1.0) |
| `positronick agent search\|show\|list\|install` | Agents | (coming in v0.1.0) |
| `positronick skill search\|show\|list\|install` | Skills | (coming in v0.1.0) |
| `positronick plugin search\|show\|list\|install` | Plugins | (coming in v0.1.0) |
| `positronick loop search\|show\|list\|install` | Agent loops | (coming in v0.1.0) |
| `positronick mcp serve` | Run the Positronick MCP server | (coming in v0.1.0) |
| `positronick login` / `logout` / `auth status` | Authenticate against positronick.com | (coming in v0.1.0) |
| `positronick init` | Bootstrap a project for agent capabilities | (coming in v0.1.0) |
| `positronick agent-docs` | Print agent-facing usage docs | (coming in v0.1.0) |
| `positronick completion` | Generate shell completions | available |
| `positronick self update` | Update the CLI in place | (coming in v0.1.0) |
| `positronick version` | Print version information | available |

## For agents

The CLI is designed to be driven by coding agents:

- **`--json`** (global flag): every command emits machine-readable JSON on stdout.
- **Error envelope**: failures with `--json` print exactly one line to stderr:

  ```json
  {"error":{"code":"not_found","message":"...","hint":"..."}}
  ```

  `hint` is omitted when empty. Codes: `error`, `cancelled`, `not_found`, `auth_required`.
- **Exit codes**: `0` ok · `1` error · `2` cancelled · `3` not found · `4` auth required.
- **Streams**: stdout carries data, stderr carries progress and errors. Pipe-safe.
- **Non-interactive auto-detection**: prompts and color are disabled automatically when stdout is not a TTY, under CI (`CI`, `GITHUB_ACTIONS`), or under a coding agent (`CLAUDECODE`, `GEMINI_CLI`, `CURSOR_EDITOR`). `NO_COLOR` and `--no-color` are honored.

## Development

```sh
go test ./...
golangci-lint run
```

## License

[Apache-2.0](LICENSE) © 2026 MiDika SRL
