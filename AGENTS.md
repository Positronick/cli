# AGENTS.md

Guidance for coding agents working in this repository.

## Commands

```sh
go build ./...        # compile everything
go test ./... -race   # full test suite (CI runs exactly this)
golangci-lint run     # lint (v2 config in .golangci.yml); zero issues required
gofmt -l .            # must print nothing
```

## Rules

- **Golden-file diff = public contract change.** The exit codes, JSON error envelope, `--json` output shapes, and table layout are scripted against by agents. If a test golden string changes, call it out explicitly in the PR description.
- **No network in unit tests.** Use `net/http/httptest` for anything HTTP-shaped. Tests must pass offline.
- **Every command's `RunE` returns a typed result through `internal/output`** — never `fmt.Print` directly in command code. `internal/output` is the single rendering authority (Printer, RenderTable, EmitJSON).
- **Errors flow up to `Execute()`; render exactly once.** Commands return errors (use `internal/output` constructors: `NotFoundError`, `AuthError`, `CancelledError`, `Errorf`) and never print them. `cli.Execute` renders via `output.RenderError` and maps to the exit code. Cobra's `SilenceErrors`/`SilenceUsage` stay on.
- **Conventional commits.** `feat:`, `fix:`, `docs:`, `chore:`, `ci:`, `test:`, `refactor:`.

## Layout

- `cmd/positronick` — `main` only; delegates to `internal/cli`.
- `internal/cli` — cobra command tree; `Execute()` owns signals + error rendering.
- `internal/output` — exit codes, error envelope, environment/TTY detection, printer, tables.
- `internal/version` — build metadata injected via ldflags.

Planned (later PRs): `internal/api` (HTTP client), `internal/search`, `internal/auth`, `internal/install`, `internal/mcpserver`, noun commands.
