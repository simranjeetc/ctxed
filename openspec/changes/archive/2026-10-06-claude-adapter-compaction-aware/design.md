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

### Preserved messages are live (found while implementing)

Claude Code 2.1.291 writes `compactMetadata.preservedMessages.uuids` on the
boundary: lines from before it (in practice the last assistant turn) that it
re-attaches after the summary. Those are in the live context, so the live view
is: summary, preserved entries (source order), then the entries after the
boundary. Older versions write no list and keep nothing. Dropping a preserved
entry removes its line like any other.

### Writing is byte-exact

`Write` used to append a newline after every split piece, so every write added a
blank final line. It now rejoins the pieces as split, so an unedited document is
written back byte-for-byte.

## Open Questions (answered in the adapter package comment)

- Does Claude Code ever write entries *after* the boundary that belong to the
  pre-compaction context (e.g. late async flushes)? **No.** The post-boundary
  lines with earlier timestamps are the summary and the `/compact` command's own
  lines.
- Are there sidechain (`isSidechain: true`) entries for subagents in the same
  file? **No.** Subagents are written to `<session>/subagents/agent-*.jsonl`.

## Non-Goals

- Changing how Claude Code compacts.
- Any OpenCode adapter change.
- A full-history view (an `inspect --include-compacted` audit flag was built,
  then dropped as not needed; add it later if someone asks).
