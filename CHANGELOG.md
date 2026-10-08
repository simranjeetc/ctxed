# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `ctxed update`: replace the binary with the latest release, verified against
  the release's `checksums.txt`. `--check` reports without installing; `--force`
  reinstalls.

### Changed

- README is skill-first: it shows how to ask for the overview and how to read
  the output. It no longer lists CLI commands or flags.
- The full command-line reference moved to `docs/cli.md`.

## [0.1.0] - 2026-10-08

### Added

- `ctxed overview`: show what a session's context is made of — which topics it
  holds, how many tokens each takes, which are done, and what is still pending.
  Works the same in Claude Code and OpenCode.
- `ctxed inspect`: list the live context entry by entry, with role, kind, token
  count and a preview. `--json` emits the same fields as an object.
- `ctxed categorize`: group the entries into categories and write an editable
  `<name>.categories.json`.
- `install.sh`: install the `ctxed` binary and the `ctxed-overview` skill without
  cloning the repo.
- Release archives (linux/darwin, amd64/arm64) with checksums via goreleaser.

[Unreleased]: https://github.com/simranjeetc/ctxed/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/simranjeetc/ctxed/releases/tag/v0.1.0
