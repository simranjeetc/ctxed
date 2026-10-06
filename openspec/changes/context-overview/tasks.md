# Tasks

## 1. Live view and session lookup

- [x] 1.1 Move the OpenCode live view (after last compaction) into the OpenCode adapter; `inspect` uses it
- [x] 1.2 Count the compaction record (summary + kept tail) as its own row in both harnesses
- [x] 1.3 Session lookup: file arg, `--session`, `CLAUDE_CODE_SESSION_ID`, `OPENCODE_SESSION_ID`; move export and id validation out of `ocprune`

## 2. Overview

- [x] 2.1 Extend the categorize prompt and parser with `status` and `pending`
- [x] 2.2 `ctxed overview` with table and `--json` output; usage text
- [x] 2.3 Unit and CLI tests: totals add up and match `inspect`; pre-compaction entries not counted; file unchanged; missing status prints `?`; no session exits 2

## 3. Park pruning

- [x] 3.1 Remove `prune`, `compact-instruction`, `drop`, `opencode` from CLI dispatch and usage
- [x] 3.2 Uninstall the global OpenCode plugin and `ctxed-prune` skill; remove the prune skills from the repo's skill dirs
- [x] 3.3 README and docs: describe the overview; note pruning is parked

## 4. Skill and verification

- [x] 4.1 `ctxed-overview` skill (one file, installed for Claude Code and OpenCode)
- [x] 4.2 `verify-functionally.sh`: replace prune scenarios with overview scenarios for both harnesses (deterministic totals + read-only checks with a stub categorizer; one real cheap-model status check)
- [x] 4.3 Run both scenarios and report results (2026-10-06: `--all` 18 hard pass, 2 soft pass)
