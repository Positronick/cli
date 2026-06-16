# AGENTS.md

Guidance for coding agents working in this repository.

## Commands

```sh
go build ./...        # compile everything
go test ./... -race   # full test suite (CI runs exactly this)
make lint             # golangci-lint at the CI-pinned version; zero issues required
make check            # everything CI runs (tidy/fmt/lint/vuln/shellcheck/actionlint/race tests/cross-builds)
```

Tool versions are pinned in the Makefile (synced with `.github/workflows/ci.yml`) —
prefer the make targets over bare `golangci-lint`/`govulncheck` so local results
match CI.

## Rules

- **Golden-file diff = public contract change.** The exit codes, JSON error envelope, `--json` output shapes, and table layout are scripted against by agents. If a test golden string changes, call it out explicitly in the PR description.
- **No network in unit tests.** Use `net/http/httptest` for anything HTTP-shaped. Tests must pass offline.
- **Every command's `RunE` returns a typed result through `internal/output`** — never `fmt.Print` directly in command code. `internal/output` is the single rendering authority (Printer, RenderTable, EmitJSON).
- **Errors flow up to `Execute()`; render exactly once.** Commands return errors (use `internal/output` constructors: `NotFoundError`, `AuthError`, `CancelledError`, `Errorf`) and never print them. `cli.Execute` renders via `output.RenderError` and maps to the exit code. Cobra's `SilenceErrors`/`SilenceUsage` stay on.
- **Conventional commits.** `feat:`, `fix:`, `docs:`, `chore:`, `ci:`, `test:`, `refactor:`.
- **Repo-root artifacts are pinned from `cmd/positronick`.** Tests that guard a repo-root file against Go exports (.goreleaser.yaml/install.sh in `release_contract_test.go`, skills/positronick/SKILL.md in `skill_contract_test.go`) live there — not in `internal/*`. Add new drift guards to that package.

## Layout

- `cmd/positronick` — `main` only; delegates to `internal/cli`. Its e2e test builds the real binary against `internal/mockapi`.
- `internal/cli` — cobra command tree (noun commands, `agent-docs`); `Execute()` owns signals + error rendering. Golden files live in `internal/cli/testdata/golden/`; regenerate with `go test ./internal/cli -update`.
- `internal/api` — typed HTTP client for the positronick.com API; wire types mirror the product repo's `src/lib/types.ts`.
- `internal/auth` — device-flow login, the credential cache (`credentials.json`, 0600, keyed to its base URL), and the `api.CredentialsProvider` every command authenticates through (env `POSITRONICK_API_KEY` > cached bearer > anonymous).
- `internal/search` — 1:1 Go port of the website's fuzzy ranking (`src/lib/filter.ts` / `listingFilter.ts`); change those first, mirror here.
- `internal/config` — base-URL/config-dir resolution (flag > env > config file > default).
- `internal/install` — SOUL.md install conventions: harness detection (`.hermes`/`.claude`/`.cursor`/`.openclaw` in cwd, then home), the per-target path table, the overwrite gate (the counter-bumping `.md` fetch runs only after the gate passes), and the `positronick-install-receipt.json` contract shared with install.sh.
- `internal/selfupdate` — `self update` machinery: install-method classification (only the installer receipt authorizes an in-place replace), GitHub releases lookup (base URL injectable for tests), checksums.txt verification, atomic binary swap.
- `internal/mcpserver` — the embedded MCP stdio server behind `mcp serve`: five consolidated tools (soul_search, soul_show, soul_install, listing_search, listing_show), thin wrappers over the same api/search/install packages the commands use. The MCP SDK dependency stays confined here; `internal/cli/mcp_serve.go` is just glue, and the tools/list JSON is golden-pinned (`mcp-tools-list.json`).
- `internal/mockapi` — frozen API fixture served over httptest for golden + e2e tests; changing a fixture value is a contract-test change. Read tests use `Handler()` (418 on `.md`); install tests opt into `InstallHandler()`.
- `internal/output` — exit codes, error envelope, environment/TTY detection, printer, tables.
- `internal/version` — build metadata injected via ldflags.

## Harness integration (Hermes)

`hermes` is positronick's default harness target (`internal/install`). Two integration surfaces — both end-user steps live in the README's "Use with Hermes"; the code behind them is here:

- **MCP** — `hermes mcp add positronick --command positronick --args mcp serve` loads the five tools from `internal/mcpserver` over stdio. Keep `mcp-tools-list.json` golden in sync (it is the registered tool contract).
- **Skill** — the bundled [`skills/positronick/SKILL.md`](skills/positronick/SKILL.md) drops into a Hermes profile's `skills/` dir and is auto-discovered; the agent then drives the CLI. Its drift guard is `skill_contract_test.go` (see the repo-root-artifacts rule above).
