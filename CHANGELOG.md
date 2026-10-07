# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-10-07

### Added

- `ctxed overview`: show what a session's context is made of — which topics it
  holds, how many tokens each takes, which are done, and what is still pending.
  Works the same in Claude Code and OpenCode.
- `ctxed inspect`: list the live context entry by entry, with role, kind, token
  count and a preview. `--json` emits the same fields as an object.
- `ctxed categorize`: group the entries into categories and write an editable
  `<name>.categories.json`.

[Unreleased]: https://github.com/simranjeetc/ctxed/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/simranjeetc/ctxed/releases/tag/v0.1.0
