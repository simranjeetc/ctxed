# Proposal

## Why

ctxed can emit a pruned transcript, but nothing applies it. A prune still has to
be performed by hand, so the point — a session that continues with a slimmer
context — is not yet real. OpenCode exposes `session.hook("context", handler)`,
an outbound-transcript hook (the one DCP uses), so a small plugin can apply a
ctxed prune at dispatch and let the session continue. The plugin must stay thin:
categorization and pruning already live in ctxed; the plugin only asks and
substitutes.

**Claude Code is not covered by this change.** Its hook surface
(`~/.claude/settings.json`: `SessionStart`, `PreToolUse`, and so on) cannot
rewrite the outbound message list, so dispatch-time pruning is not expressible
as a Claude Code hook. That harness needs a different mechanism — editing the
transcript file it resumes from, or a wrapper — and is deferred to its own
change.

## What Changes

- A new OpenCode plugin (TypeScript, `@opencode/plugin`, the v2 API that
  exposes `session.hook("context")`) that hooks
  `session.hook("context")`, obtains the resolved prune set from ctxed, and
  removes those messages from the outbound transcript for that dispatch.
- A small, additive ctxed flag: `prune --ids-only` prints the resolved set of
  **dropped** entry ids (after orphan resolution) instead of a transcript. The
  plugin needs the dropped set, not the kept set, so it can filter live messages
  by id without re-serialising the transcript on every dispatch.
- Configuration: ctxed's location, the session export, and the categories file
  plus the selected categories or explicit ids.
- No change to ctxed's existing behavior; the flag is additive.

## Capabilities

### New Capabilities

- `opencode-dispatch-plugin`: apply a ctxed prune to the outbound transcript at
  dispatch — non-destructively, fail-open, and with no logic of its own.

### Modified Capabilities

- `context-dispatch-prune`: `prune` gains an id-only output mode that reports the
  resolved set of dropped entry ids (after orphan resolution), so a dispatch
  plugin can filter the outbound messages by id instead of re-serialising a
  transcript on every dispatch. This is the delta in
  `specs/context-dispatch-prune/spec.md`.

## Impact

- New: `plugin/opencode/` (TypeScript) with a pin to the plugin API version, plus
  a node test that exercises the id-filter with a stub ctxed.
- ctxed: `prune --ids-only` (additive), covered by a test.
- Runtime: the plugin shells out to `ctxed`; the binary must be on `PATH` or
  configured.
- OpenCode's plugin API is beta; the plugin pins a version.
- Out of scope: Claude Code, any automatic/unattended pruning policy, and any
  other change to ctxed.
