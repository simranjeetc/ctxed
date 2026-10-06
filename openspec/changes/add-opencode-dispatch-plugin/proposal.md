# Proposal

## Why

ctxed can decide what to drop, but nothing applies that decision to a running
session. In OpenCode the user has to export a session, categorize it, pick
buckets, and hope the selection still matches — none of which happens inside the
session they are in.

This change makes the whole workflow happen inside the session: the plugin
categorizes the **live** conversation, presents the buckets, takes the user's
bucket selection, and applies it to every subsequent dispatch. Categorization is
a step of the prune, not a separate tool run on a stale export.

**Claude Code is not covered by this change.** Its settings hooks cannot rewrite
the outbound message list, and a mod's `turn.step` event pins the transcript, so
dispatch-time pruning is not expressible there; its live mechanism is compaction
(`docs/live-context-prune-decisions.md`, `add-claude-code-live-prune`).

## What Changes

- The OpenCode plugin gains an **in-session ctxed command**: it categorizes the
  live messages (via ctxed), presents the buckets, and records the user's bucket
  selection. Buckets, not ids, are what the user picks.
- The dispatch hook applies the recorded selection to **every** dispatch, by
  resolving the dropped set over the live transcript (cached by session
  revision). Messages added after the selection are kept, even on a dropped
  topic.
- ctxed gains the inputs this needs: a way to categorize an arbitrary transcript
  and to print the resolved dropped ids (`prune --ids-only`).
- Configuration: ctxed's location, the categorizer model/transport, and the
  active selection.
- No change to ctxed's existing file-based behavior.

## Capabilities

### New Capabilities

- `opencode-dispatch-plugin`: run categorize → select → apply from inside an
  OpenCode session, with the selection covering later messages, non-destructively
  and fail-open.

### Modified Capabilities

- `context-dispatch-prune`: adds the id-only output mode so the plugin can filter
  outbound messages by id (delta in `specs/context-dispatch-prune/spec.md`).

## Impact

- `plugin/opencode/` (TypeScript): the in-session command, the dispatch hook, the
  live-transcript hand-off to ctxed, and the selection store.
- ctxed: `prune --ids-only`, and a transcript input path for categorizing live
  messages (additive).
- Runtime: the plugin shells out to `ctxed`; the binary must be on `PATH` or
  configured. Categorization needs a model (endpoint or `--categorizer-cmd`).
- OpenCode's plugin API is beta; the plugin pins a version.
- Out of scope: Claude Code, automatic/unattended pruning, any other ctxed change.

## Verification

Functional, against a live OpenCode session (see `scripts/verify-functionally.sh`
and the strategy in `docs/verification-strategy.md`): create a session, run the
in-session categorize+select, dispatch, and confirm the dropped bucket is absent
from the request, a message added after the selection is kept, and the stored
session is unchanged.
