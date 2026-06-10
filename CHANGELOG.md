# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Read commands: `soul` and the seven registry nouns (`harness`, `cli`, `mcp`, `agent`, `skill`, `plugin`, `loop`), each with `search`/`list`/`show` (`--limit`, `--sort`, `--category`, souls also `--framework`; `soul show --raw` prints the SOUL.md body verbatim; `loop show` renders the loop recipe). Golden-file-pinned `--json` output, did-you-mean hints on exit 3, and the `agent-docs` self-description command.
- CLI scaffold: cobra root command, `version` subcommand, output/exit-code contract (`--json` error envelope, exit codes 0–4), environment auto-detection (TTY/CI/agent/NO_COLOR), CI workflow.

### Fixed

- **install.sh**: tolerate v-prefixed `POSITRONICK_VERSION` pins, resolve "latest" to a concrete release tag before downloading (tarball and checksums always come from the same release, and a clear error is shown when no release exists yet), and match the checksum entry by exact asset name in both sha256sum output modes.
