# Proposal

## Why

Claude Code does **not** rewrite the `.jsonl` transcript when it compacts. It
appends a `system` entry with `"subtype":"compact_boundary"` (carrying
`compactMetadata: {trigger, preTokens, postTokens, …}`) followed by a user entry
marked `isCompactSummary` that holds the summary. Verified on this machine in
transcripts created by `scripts/verify-functionally.sh --claude`, e.g.:

```json
{"subtype":"compact_boundary","content":"Conversation compacted",
 "compactMetadata":{"trigger":"manual","preTokens":22040,"postTokens":2512, …}}
```

The Claude adapter (`internal/adapter/claude/claude.go`) has no handling for
either marker. Consequences:

- `ctxed inspect` on a compacted session counts and costs entries that are no
  longer in the live context; the token total is wrong.
- `ctxed categorize` buckets pre-compaction entries. A user can "drop" a topic
  that is already gone, and `compact-instruction` names it.
- The verifier's `claude:session changed` check
  (`scripts/verify-functionally.sh`, `claude_scenario`) compares entry counts.
  Because compaction appends, the count always changes; the check cannot fail.

## What Changes

- The Claude adapter treats the last `compact_boundary` as the start of the live
  context. Entries before it are excluded from `inspect`, `categorize`,
  `prune` and `compact-instruction` by default.
- Messages the boundary lists as preserved (`preservedMessages.uuids`, Claude
  Code 2.1.291+) stay in the live view, right after the summary.
- The compact summary entry is parsed as kind `summary` (already a canonical
  kind in `context-session-format`).
- Round-trip (`drop`) still writes every line, including pre-boundary lines,
  byte-for-byte; only the entry view changes.
- The Claude functional scenario asserts on the boundary instead of entry count:
  a new `compact_boundary` exists after `/compact`, `postTokens < preTokens`, and
  the summary entry is present.

## Capabilities

### Modified Capabilities

- `context-session-format`: adds a requirement that a compacted Claude Code
  transcript exposes only the live context.

## Impact

- `internal/adapter/claude/claude.go` and tests; new fixture
  `testdata/claude_session_compacted.jsonl` (trimmed from a real compacted
  transcript; scrub content).
- `internal/cli` tests only (no new flag).
- `scripts/verify-functionally.sh` `claude_scenario` assertions.
- `.claude/skills/ctxed-prune-context/SKILL.md`: drop the caveat implying
  stale pre-compaction buckets are expected.
