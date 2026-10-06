# Proposal

## Why

The OpenCode plugin does everything itself: it categorizes, prints the buckets,
takes the reply and filters each request. Running it in a real session showed
two problems:

- **The model answers the plugin.** Every notice the plugin posts
  ("Categorizing…", the bucket list, an error) is a synthetic message, and
  OpenCode starts a model turn on each one. The user sees unprompted replies
  such as "Drop 1, 2, 3, 5. Keep 4."
- **The selection silently stops applying.** The plugin categorizes the full
  session history, including `system` items that never reach the request.
  At dispatch, ctxed then refuses the categories file ("unknown entry id") and
  the plugin, failing open, drops nothing on every turn.

Also, running the prune a second time sorts the whole history again, so dropped
topics come back in the list, and a new pick replaces the old one instead of
adding to it.

## What Changes

- **ctxed does the thinking.** Two new commands:
  - `ctxed opencode categorize` finds the session (`$OPENCODE_SESSION_ID`, set
    by OpenCode for every shell command it runs), exports it, and sorts only what
    the model still sees: messages after the last OpenCode compaction, minus
    everything already dropped. It prints a numbered list.
  - `ctxed opencode drop 1,3` adds those categories' messages to the session's
    drop list. The list only grows.
  Both share one state file per session.
- **A skill does the asking.** `ctxed-prune` runs the two commands and offers
  the categories through OpenCode's `question` tool as a multi-select.
- **The plugin only filters.** It reads the session's drop list before each
  request and removes those messages. It posts nothing, registers no command,
  calls no binary, and holds no state.
- **Verification**: the functional suite drives the same commands the skill
  runs, prunes twice, and checks across an OpenCode compaction, with hard
  codeword checks.

## Capabilities

### New Capabilities

- `opencode-skill-prune`: the skill-driven prune, the cumulative drop list, and
  what is sorted on each prune.

### Modified Capabilities

- `opencode-dispatch-plugin`: the plugin shrinks to a filter over the drop list.

## Impact

- New Go package `internal/ocprune` and the `ctxed opencode` subcommands.
- `plugin/opencode` loses the command, the reply handling, the ctxed runner and
  the transcript translation.
- New skill under `plugin/opencode/skill/ctxed-prune/`.
- `scripts/verify-functionally.sh`: the OpenCode scenarios are rewritten; the
  self-config scenario goes, since the plugin no longer resolves anything.
- Out of scope: Claude Code, undo/clear of a drop list, categorizer quality.
