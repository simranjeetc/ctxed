# Parked: pruning a live session

Kept for reference; nothing here is reachable from the `ctxed` binary, a shipped
skill, or an installed plugin.

| What | Where |
|---|---|
| CLI commands `drop`, `prune`, `compact-instruction`, `opencode` | `internal/cli` (`runDrop`, `runPrune`, `runCompactInstruction`, `runOpenCode`); removed from dispatch. Their tests run through the test-only `cli.RunWithParked` (`internal/cli/export_test.go`). |
| Packages | `internal/prune`, `internal/compact`, `internal/ocprune` — tested by `go test ./...` |
| OpenCode plugin and `ctxed-prune` skill | `plugin/opencode/` — tested by `scripts/verify.sh` |
| Claude Code `ctxed-prune-context` skill | `parked/claude-skill/` |
| Live prune scenarios | `parked/verify-prune-functionally.sh` (needs the parked commands; does not run against the current binary) |

Why: compaction in both harnesses brought dropped content back (OpenCode keeps
a verbatim tail outside the plugin's filter; Claude Code's `/compact` prompt
keeps every user message). See `openspec/changes/context-overview/proposal.md`.

To unpark, restore the dispatch cases in `internal/cli/cli.go` and move the
skill back to `.claude/skills/`.
