# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`positronick research`**: the agent-facing "what's new" feed over positronick.com's published blog posts, mirrored GitHub releases, and mirrored news links — newest first, so agents avoid stale knowledge. Filter with `--kind` (article/release/link), `--category`, `--tag`, or a free-text query, and poll just the delta with `--since <iso>`; the printed `latest` timestamp is the value to pass back next time. Read-only and unauthenticated, like the soul/listing reads. Backed by the public `GET /api/research` endpoint.
- **`positronick blog`**: read the positronick.com blog from the terminal — `blog list` (newest first, optional `--kind` article/release/link) and `blog show <slug>`, with `--raw` printing the markdown body verbatim and did-you-mean hints (exit 3) on a missing slug. Read-only and unauthenticated, mirroring the soul/listing reads. Backed by the public `GET /api/blog`, `/api/blog/{slug}`, and `/api/blog/{slug}.md` endpoints.

## [0.1.2] - 2026-06-16

### Added

- **MCP usage instructions**: the `mcp serve` initialize response now carries usage guidance for the client's model — every connected MCP client teaches its agent the soul/listing workflow with no install step.
- **Agent skill**: `skills/positronick/SKILL.md` teaches agents the full search → show → install workflow and the CLI's `--json`/exit-code contract; to be published on the registry itself as the `positronick` skill listing, with a contract test pinning its target/type enumerations to their Go sources.
- **Package managers**: Homebrew cask (macOS & Linux, Homebrew ≥ 4.5), npm (`npm install -g positronick`, macOS & Linux), Scoop and Winget (Windows), and AUR (`positronick-bin`) publishing channels; each goes live as its publishing credentials land.
- **Hermes integration guide**: a README "Use with Hermes" section and an AGENTS.md note covering both ways to wire the CLI into a Hermes agent — registering the embedded MCP server (`hermes mcp add --command positronick --args mcp serve`) or installing the bundled skill (`hermes skills install`); every command validated against a live Hermes agent.
- **`positronick profile` (admin)**: create and list registry authoring profiles — `profile create --handle <h> --name <n>` (`--kind`, `--avatar-url` defaulting to `https://github.com/<handle>.png`, `--verified`/`--official`) and `profile list`. Lets an admin author a listing under a brand-new profile instead of requiring it be git-curated first. Backed by the new `POST`/`GET /api/admin/profiles` API.

#### Internal
<!-- internal -->
- npm packaging pipeline: `scripts/build-npm-packages.sh` builds the four platform packages + meta-package from release archives; published via npm OIDC Trusted Publishing (no token secret), gated on the `NPM_PUBLISH` repo variable until the one-time bootstrap; validated in CI and `make check`.

## [0.1.1] - 2026-06-11

### Fixed

- **Device-flow login**: polling now survives HTTP 429 responses — it backs off, honors `Retry-After`, and keeps polling instead of aborting.

## [0.1.0] - 2026-06-11

### Added

- **Read commands**: `soul` and the seven registry nouns (`harness`, `cli`, `mcp`, `agent`, `skill`, `plugin`, `loop`), each with `search`/`list`/`show` (`--limit`, `--sort`, `--category`, souls also `--framework`; `soul show --raw` prints the SOUL.md body verbatim; `loop show` renders the loop recipe). Golden-file-pinned `--json` output, did-you-mean hints on exit 3, and the `agent-docs` self-description command.
- **`install <slug>`**: install a registry listing's assets locally.
- **CLI scaffold**: cobra root command, `version` subcommand, output/exit-code contract (`--json` error envelope, exit codes 0–4), environment auto-detection (TTY/CI/agent/NO_COLOR), CI workflow.
- **Authentication**: `auth login` (device flow), API-key auth, and an OS-native credential store (`auth status`/`logout`/`token`).
- **`mcp serve`**: embedded stdio MCP server that exposes the registry to any MCP client.
- **Self-management**: `init` and `self update` (binary self-update), backed by a one-line `install.sh` installer and a goreleaser release pipeline that publishes signed `checksums.txt` and a sigstore attestation.
- **Admin commands**: hidden `soul` and `listing` create/update for registry authoring.
- **SECURITY.md**: private vulnerability reporting policy and release verification instructions (`gh attestation verify` + `checksums.txt`).

### Fixed

- **install.sh**: tolerate v-prefixed `POSITRONICK_VERSION` pins, resolve "latest" to a concrete release tag before downloading (tarball and checksums always come from the same release, and a clear error is shown when no release exists yet), and match the checksum entry by exact asset name in both sha256sum output modes.
- **Did-you-mean hints**: never suggest the user's own input slug back to them.

#### Internal
<!-- internal -->
- Makefile dev loop mirroring CI (`make check`: tidy/fmt/lint/vuln/shellcheck/actionlint/race tests/cross-builds) with tool versions pinned to match CI.
- Release-contract test pinning the `positronick_<os>_<arch>` / `checksums.txt` asset naming shared by .goreleaser.yaml, install.sh and self-update (new `selfupdate.AssetName`/`ChecksumsName` exports).
- CI hardening: all actions SHA-pinned, `persist-credentials: false`, per-job release permissions, PR-run cancellation, new govulncheck + hygiene (tidy drift, shellcheck, actionlint) checks, weekly grouped Dependabot for gomod and actions.

[Unreleased]: https://github.com/Positronick/cli/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/Positronick/cli/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/Positronick/cli/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Positronick/cli/releases/tag/v0.1.0
