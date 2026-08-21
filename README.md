# Positronick CLI

`positronick` discovers and installs agent capabilities — souls, harnesses, CLIs, MCP servers, memory, agents, skills, plugins, loops, and bots — from [positronick.com](https://positronick.com).

## Install

```sh
curl -fsSL https://positronick.com/install.sh | sh
```

> The install script goes live with v0.1.0.

Or with Go:

```sh
go install github.com/positronick/cli/cmd/positronick@latest
```

### Package managers

Rolling out alongside v0.1.0 (each channel goes live as its publishing
credentials land — see `.goreleaser.yaml` and `packages/npm/README.md`):

```sh
brew install positronick/tap/positronick          # macOS & Linux (Homebrew ≥ 4.5)
npm install -g positronick                        # macOS & Linux — or: npx positronick
scoop bucket add positronick https://github.com/Positronick/scoop-bucket
scoop install positronick                         # Windows
winget install Positronick.Positronick            # Windows
yay -S positronick-bin                            # Arch (AUR)
```

## Usage

| Command | Description | Status |
| --- | --- | --- |
| `positronick soul search\|show\|list\|install` | Find and install `SOUL.md` personality files | available |
| `positronick harness search\|show\|list\|install` | Agent harnesses | available |
| `positronick cli search\|show\|list\|install` | CLI tools | available |
| `positronick mcp search\|show\|list\|install` | MCP servers | available |
| `positronick memory search\|show\|list\|install` | Memory and context engines | available |
| `positronick agent search\|show\|list\|install` | Agents | available |
| `positronick skill search\|show\|list\|install` | Skills | available |
| `positronick plugin search\|show\|list\|install` | Plugins | available |
| `positronick loop search\|show\|list\|install` | Agent loops | available |
| `positronick bot search\|show\|list\|install` | Scheduled agent bots | available |
| `positronick mcp serve` | Run the Positronick MCP server | (coming in v0.1.0) |
| `positronick login` / `logout` / `auth status` / `auth token create` | Authenticate against positronick.com | available |
| `positronick init` | Detect your harness, install a soul, suggest tooling | available |
| `positronick agent-docs` | Print agent-facing usage docs | available |
| `positronick completion` | Generate shell completions | available |
| `positronick self update` | Update the CLI in place | available |
| `positronick version` | Print version information | available |

## Authentication

Browsing is anonymous; authenticate for account features. Three setups:

### On your laptop

```sh
positronick login
```

Prints a short code and auto-opens the verification page in your browser (macOS, or Linux with a display) — approve, done. The session is cached in `~/.config/positronick/credentials.json` (mode 0600), keyed to the API host that issued it. Sessions last about 7 days, sliding with use; when one expires (HTTP 401), just `positronick login` again.

### On a server, over SSH

```sh
positronick login --no-browser
```

Copy the printed code and open the verification URL on any device — your phone works fine. The session lands on the machine where you ran `login`. (Auto-open is already skipped when there is no display; `--no-browser` makes it explicit.)

### Unattended (CI, cron, agents)

Mint an API key from a logged-in machine, then export it where the CLI runs:

```sh
positronick auth token create --name ci --expires-days 90
export POSITRONICK_API_KEY=posi_...
```

The raw key is printed exactly once — the server stores only a hash. `POSITRONICK_API_KEY` always wins over a cached login and is never written to disk. Creating keys requires a logged-in session: an API key cannot mint more keys.

Check who you are with `positronick auth status` (live check; exits 0 in every resolvable state). `positronick logout` deletes the cached session.

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
- **MCP server**: `positronick mcp serve` speaks MCP over stdio (`claude mcp add positronick -- positronick mcp serve`). The initialize response carries usage instructions for the client's model.
- **Agent skill**: [`skills/positronick/SKILL.md`](skills/positronick/SKILL.md) teaches agents the full search → show → install workflow; it is published on the registry itself as the `positronick` skill listing once released.

## Use with Hermes

Hermes treats `positronick` as a first-class harness target (it is the default
when no harness is specified). After [installing](#install) the binary, wire it
into a Hermes agent one of two ways. Both work with anonymous reads; see
[Authentication](#authentication) for installs and publishing.

**As an MCP server (recommended)** — gives the agent the five Positronick tools
natively (`soul_search`, `soul_show`, `soul_install`, `listing_search`,
`listing_show`):

```sh
hermes mcp add positronick --command positronick --args mcp serve
hermes mcp test positronick   # ✓ Connected — 5 tools
```

Start a new agent session to pick up the tools. To install or publish from
inside the agent, add `--env POSITRONICK_API_KEY=posi_...` to the command above
so the stdio server inherits the token.

**As a skill** — so the agent drives the `positronick` CLI through its terminal
tool. Hermes fetches, security-scans, and enables the bundled skill:

```sh
hermes skills install https://raw.githubusercontent.com/positronick/cli/main/skills/positronick/SKILL.md --yes
hermes skills list   # positronick → enabled
```

Offline or working from a clone, copy the file into any category folder under
your active profile instead:
`cp skills/positronick/SKILL.md ~/.hermes/profiles/<profile>/skills/agent-tooling/positronick/SKILL.md`.

## Verifying releases

Release archives ship with `checksums.txt` (SHA-256) and sigstore-backed build
provenance; the install script verifies checksums automatically. See
[SECURITY.md](SECURITY.md) for manual verification commands and the
vulnerability reporting policy.

## Development

```sh
make check   # everything CI runs: tidy/fmt/lint/vuln/shellcheck/actionlint/race tests/cross-builds
make help    # list all targets
```

## License

[Apache-2.0](LICENSE) © 2026 [MiDika SRL](https://midika.it)
