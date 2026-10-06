# Tasks

## 1. ctxed `compact-instruction` (D3a)

- [x] 1.1 Add `internal/compact` rendering a single `/compact` instruction from a categories file plus the selected category ids: name the kept buckets (unselected) and the dropped buckets (selected) by label; reuse `categorize.Select` so an unknown id errors as in `prune`
- [x] 1.2 Unit-test the renderer: two of three buckets; a single bucket; the serial-comma list; all buckets dropped; an unknown id; a missing id among valid ones; determinism; empty-label fallback
- [x] 1.3 Wire `ctxed compact-instruction <session> --categories-file F --categories 1,3` into `internal/cli`, reading the session only to validate the categories file; usage/exit codes match the other commands
- [x] 1.4 CLI tests: labels of the kept/dropped buckets appear; output is one line; unknown category refused; missing `--categories` is a usage error; deterministic; the session file is unchanged; works with stdin closed
- [x] 1.5 Document the flow in the README (`categorize` → pick buckets → `compact-instruction` → `/compact <text>`) and in `docs/adapters.md` Claude Code notes

## 2. Claude Code compact mod (D3b) — dropped

Dropped 2026-10-06. Scope is now one flow per harness, verified by a codeword
test (`scripts/verify-functionally.sh --claude`); the `/compact` instruction is
the Claude Code path. If that test shows a dropped topic surviving compaction,
the mod is the fix and comes back as its own change.

## 3. Acceptance

- [x] 3.1 Verify `openspec validate add-claude-code-live-prune --strict` passes and both delta specs match the implemented D3a behavior
- [x] 3.2 Verify `go test ./...` is green and `go vet ./...` is clean
