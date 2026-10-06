# Design

## Decisions

### Live view starts at the last boundary

Parse all lines (needed for round-trip), then expose as entries only those after
the last line with `type == "system" && subtype == "compact_boundary"`. The
summary entry (`isCompactSummary: true`) becomes the first entry, kind `summary`,
role `user`.

Entry indices are assigned over the live view, so `--indices` refers to what the
user sees in `inspect`.

### Writing preserves everything

`drop` removes only the chosen live entries' lines. Lines before the boundary
are written unchanged. Dropping the summary entry is allowed (it is an ordinary
entry), but dropping the boundary line itself is not possible because it is not
an entry.

### Audit flag

`inspect --include-compacted` shows the full history with a column marking
entries before the boundary. It is read-only; `drop`, `prune`, `categorize` do
not accept it (keep the surface small; add later if needed).

## Open Questions (verify before coding)

- Does Claude Code ever write entries *after* the boundary that belong to the
  pre-compaction context (e.g. late async flushes)? Inspect several real
  compacted transcripts under `~/.claude/projects` and record the finding in the
  adapter's package comment.
- Are there sidechain (`isSidechain: true`) entries for subagents in the same
  file? If so, decide whether they are part of the live main context (likely
  not) and test it.

## Non-Goals

- Changing how Claude Code compacts.
- Any OpenCode adapter change.
