---
name: positronick
description: >-
  Discover and install agent capabilities from the positronick.com registry —
  souls (SOUL.md personality files), harnesses, CLIs, MCP servers, memory,
  agents, skills, plugins, loops, and bots. Use when asked to find or install a soul or
  SOUL.md, browse positronick listings, set up a loop recipe, or whenever the
  positronick MCP tools or `positronick` CLI are available and the task
  involves agent tooling discovery.
---

# Using positronick

positronick.com is a registry of agent capabilities. You reach it two ways:

- **MCP server** (`positronick mcp serve`) — five tools: `soul_search`,
  `soul_show`, `soul_install`, `listing_search`, `listing_show`.
- **CLI** (`positronick`, alias `pck`) — same data, plus auth and admin.
  Run `positronick agent-docs` for its full self-describing manual.

Prefer the MCP tools when they are connected; fall back to the CLI otherwise.

## Souls: search → show → install

Souls are installable SOUL.md personality files for coding agents.

1. `soul_search` — fuzzy-ranked cards (slug, name, tagline, category,
   frameworks, version, downloads). Filter with `category` / `framework`;
   default limit 10.
2. `soul_show` — the full record including the verbatim SOUL.md body. Always
   read it before installing; search cards are deliberately slim.
3. `soul_install` — writes the soul where the target harness reads it.
   - `target` is one of `hermes`, `claude`, `cursor`, `openclaw`; when both
     `target` and `path` are omitted, it is detected from marker directories
     (`.hermes`/`.claude`/`.cursor`/`.openclaw` in cwd, then home),
     defaulting to `hermes`. Cursor is project-local — the soul lands in
     `./.cursor/rules/soul.mdc`, wrapped in mdc frontmatter — the other
     targets get `~/.<harness>/SOUL.md` verbatim.
   - The MCP tool **never overwrites**: an existing file is an error — pass
     a different `path` or remove the file first, after confirming with the
     user. (The CLI's `soul install` differs: it prompts before overwriting,
     or overwrites unasked with `--force`.)
   - It **counts as a public download** on positronick.com, so don't install
     speculatively; decide from `soul_show` first.
   - A relative `path` must resolve inside the user's home directory; pass an
     absolute path to install elsewhere.

## Registry listings: search → show

Everything else is a listing with one of these types: `harness`, `cli`,
`mcp`, `memory`, `agent`, `skill`, `plugin`, `loop`, `bot`.

1. `listing_search` — scope with `type`, filter with `category`. Cards carry
   the official `installCmd` and verified `sourceUrl`.
2. `listing_show` — the full record plus type-specific data. For loops that
   means the recipe (goal, check command, exit condition, max iterations) and
   a ready-to-paste kickoff prompt.

To install a listing, run its official `installCmd` — ask the user before
executing it, like any shell command from the network. The CLI equivalent is
`positronick <type> install <slug> --run`. Loops and bots are the exception:
they install as a prompt, not a file — `positronick loop install <slug>` and
`positronick bot install <slug>` (no `--run`) print the prompt to paste into
your agent.

## Driving the CLI

- Pass `--json` to any command for stable machine-readable stdout; progress
  and errors go to stderr.
- Exit codes: `0` ok, `1` error, `2` cancelled, `3` not found,
  `4` auth required. With `--json`, failures emit one error-envelope line on
  stderr: `{"error":{"code":...,"message":...,"hint":...}}` with codes
  `error`, `cancelled`, `not_found`, `auth_required`.
- Prompts and color auto-disable under agents (`CLAUDECODE`, `GEMINI_CLI`,
  `CURSOR_EDITOR`), CI, or a non-TTY; read commands never prompt.
- Auth: `POSITRONICK_API_KEY` env > cached login (`positronick login`,
  device flow) > anonymous. Reads work anonymously.

## Recovering from errors

A not-found slug returns a did-you-mean suggestion — retry with the suggested
slug or browse with the matching search tool. Tool errors never kill the MCP
session; just correct the call and continue.
