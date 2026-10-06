# Tasks

## 1. Investigate

- [x] 1.1 Inspect at least three real compacted transcripts (`grep -l compact_boundary ~/.claude/projects/*/*.jsonl`); record the boundary/summary line shapes and the answers to the design's open questions in the adapter package comment
- [x] 1.2 Add a trimmed, scrubbed fixture `testdata/claude_session_compacted.jsonl` with two compactions (so "last boundary" is exercised)

## 2. Adapter

- [x] 2.1 Expose only entries after the last `compact_boundary`; parse the `isCompactSummary` entry as kind `summary`
- [x] 2.2 Keep round-trip byte-exact: `drop` on the compacted fixture leaves pre-boundary lines unchanged
- [x] 2.3 Unit tests: live-view entry count; summary kind; two boundaries → last wins; uncompacted transcript unchanged behavior; round-trip

## 3. CLI

- [x] 3.1 ~~`inspect --include-compacted` full-history view~~ — dropped at the user's request; no CLI flag is added
- [x] 3.2 CLI tests: `inspect`/`categorize`/`prune`/`compact-instruction` on the compacted fixture see only live entries; `drop` keeps pre-boundary lines

## 4. Verification and docs

- [x] 4.1 `claude_scenario`: replace the entry-count comparison with: a new `compact_boundary` line exists after `/compact`; its `compactMetadata.postTokens < preTokens`; a summary entry follows it
- [x] 4.2 README "Choosing a harness → Claude Code" and the skill note that only the live context is categorized
- [x] 4.3 `go test ./...`, `go vet ./...`, `scripts/verify.sh`, `openspec validate claude-adapter-compaction-aware --strict` pass
