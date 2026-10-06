## Why

Pruning a live session could not be made reliable: OpenCode's compaction brings
dropped topics back through its verbatim tail, and Claude Code's `/compact`
prompt keeps every user message. What the user needs is simpler: to see what is
taking up the context, then decide themselves whether to carry on, compact, or
start a new session.

## What Changes

- New command `ctxed overview`: a read-only report of the current session's live
  context (what the model sees now), split into topics, each with message count,
  estimated tokens, share of the total and a status (`done` / `in progress`),
  plus a short list of what is still pending.
- The same command and the same output for Claude Code and OpenCode. The session
  is found from the harness's environment (`CLAUDE_CODE_SESSION_ID`,
  `OPENCODE_SESSION_ID`), or from `--session` / a file argument.
- One skill, `ctxed-overview`, for both harnesses: it runs the command and shows
  the result. Nothing else.
- **Pruning is parked**: its code stays in the tree but no command, skill or
  plugin reaches it. `prune`, `compact-instruction`, `drop` and `opencode` are
  removed from the CLI dispatch; the OpenCode plugin and the `ctxed-prune` and
  `ctxed-prune-context` skills are uninstalled and no longer shipped.
- Supersedes the unarchived changes `opencode-skill-prune` and
  `add-claude-code-live-prune`; they stay in `openspec/changes/` unarchived.

## Capabilities

### New Capabilities
- `context-overview`: read-only, per-topic view of a session's live context for
  Claude Code and OpenCode.

### Modified Capabilities
None. The pruning specs describe parked code; they are left as they are.

## Impact

- `internal/cli`: new `overview` command; dispatch for the pruning commands removed.
- `internal/categorize`: prompt and parser extended with topic status and pending items.
- OpenCode adapter: a shared live-view reader (after the last compaction) used by
  `overview` and `inspect`.
- Skills: `ctxed-overview` added; prune skills removed from the repo's skill dirs
  and from `~/.config/opencode`.
- `scripts/verify-functionally.sh`: prune scenarios replaced by overview scenarios.
