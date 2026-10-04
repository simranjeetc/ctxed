# Design

## Context

See `proposal.md`. ctxed groups a session's entries into labeled buckets
(`categorize`) and resolves a bucket selection to entry ids (`prune`). This
change binds that workflow to OpenCode's plugin API, so the prune is chosen and
applied from inside a running session.

**The constraint that shapes the plugin.** At dispatch the plugin holds live
message objects; ctxed works on a session *document*. The plugin therefore never
re-implements policy: it hands the live transcript to ctxed (via stdin) and
consumes what ctxed returns.

## Goals / Non-Goals

**Goals:**

- Run the whole workflow — categorize, select, apply — from inside the session.
- Have a bucket selection cover messages added after it was made.
- Keep the plugin policy-free: exec ctxed, present, substitute.
- Never let a ctxed problem break a turn (fail-open).
- Bounded per-dispatch cost.

**Non-Goals:**

- Claude Code (its hook surface cannot transform messages; see
  `docs/live-context-prune-decisions.md`).
- Automatic or budget-triggered pruning. The selection is the user's explicit
  choice.
- Mutating stored session history.
- A proxy or request interceptor.

## Decisions

### D1: Bind to `session.hook("context")`

The dispatch-time transcript hook is the only place a plugin can change what is
sent without sitting in front of the API.

- Alternative considered: an HTTP proxy. Rejected — vastly larger and
  unnecessary when the hook exists.

### D2: Categorization is part of the workflow, run in-session

The plugin registers an in-session ctxed command (an OpenCode command, backed by
the plugin). It categorizes the **live** messages and presents the buckets.

- Why: the user's mental model is "look at the topics in this session and drop
  some". Categorization is a step of the prune, not a separate tool run on a
  stale export. Running it in-session also means the buckets describe the
  session the user is actually in.
- Alternative considered: keep `categorize` as a prerequisite the user runs
  outside, on an export. Rejected — it drifts: messages added after the export
  are never covered by the selection.

### D3: The plugin delegates to ctxed over a live transcript

The plugin serializes the live messages to ctxed's session shape and passes them
on stdin (a temp file only if stdin is unavailable). ctxed returns the buckets;
after selection, ctxed returns the dropped ids.

- Why: all categorization, bucket assignment, selection resolution, and orphan
  handling stay in ctxed. The plugin stays thin.
- Alternative considered: the plugin classifies messages itself. Rejected —
  that is policy, and it belongs in ctxed.

### D4: A recorded selection, applied to every dispatch

Confirming a selection records it for the session. The dispatch hook applies it
to every outbound request — and because it re-derives the dropped set over the
**live** message list each time, a message added later is covered by the
selection.

- Why: this is what makes "continue in the same session" true for a long
  session, not just for the messages that existed at selection time.
- Implementation note: derive the dropped set by re-running ctxed over the live
  transcript, cached by session revision, rather than freezing an id list. A
  frozen list cannot cover new messages.

### D5: Fail-open

On any ctxed failure — non-zero exit, timeout, unparseable output — the plugin
sends the transcript unchanged and reports the error.

- Why: pruning is an optimization; it must never break the user's turn.

### D6: Caching keyed by selection and session revision

The plugin caches the dropped set keyed by the selection and the session
revision, and re-invokes ctxed only when either changes.

- Why: categorization is a model call and the hook may run every dispatch.
- Note: the OpenCode Go / Copilot / cheap-model choice for the categorizer is
  configuration.

### D7: Configuration is a sidecar selection plus categorizer config

The plugin reads the ctxed binary path (or `PATH`), the categorizer model and
transport (endpoint or `--categorizer-cmd`), and the active selection, from
config/env.

### D8: A small additive ctxed flag, `prune --ids-only`

ctxed prints a JSON object with `droppedIds` (the resolved set, after orphan
resolution) instead of a transcript.

- Why: the plugin needs the dropped set to filter by id.
- Alternative considered: compute the set in the plugin. Rejected — logic in the
  plugin.

## Risks / Trade-offs

- [Live message ids differ from export ids] → the entire id-based filter depends
  on this. The functional test proves it against a real dispatch; if it fails,
  the plugin matches on content/tool-id instead. This is the top functional
  verification item.
- [Categorization requires live ids to round-trip through ctxed] → the live
  transcript handed to ctxed must carry the ids the dispatch hook sees; verified
  by observing the request.
- [OpenCode's plugin API is beta and may change] → pin the API version.
- [A model call per selection is slow] → cache by selection and revision; make
  the categorizer model configurable.
- [ctxed not installed or not on PATH] → path is configurable; a missing binary
  is a fail-open no-op with a reported error.

## Migration Plan

Additive: a new `plugin/opencode/` directory and one new ctxed flag. Removing the
plugin restores default OpenCode behavior. Nothing in a session store is
modified, so there is no destructive failure mode.

## Open Questions

- The exact field names in the context hook's payload and of the live message
  id: pinned against a real dispatch by the functional test.
- Which cheap model to default the categorizer to (OpenCode Go vs Copilot).

## Implementation findings (2026-10-04)

Verified against OpenCode 2.0.19; see `docs/opencode-plugin-spike.md` for the
full record.

- **The dispatch hook works.** `session.hook("context")` fires, `event.messages`
  is mutable, and a dropped message is genuinely absent from what the model
  receives (proved live: the model answered "unknown" to a token it was never
  shown). The plugin loads only as a single flat file under
  `.opencode/plugins/` and only when the project has an `opencode.json`.
- **Blocked: driving the in-session command headlessly (task 4.1).**
  `opencode run "/ctxed-prune"` sends the text to the model; it does not execute
  the plugin command, and `opencode command list` does not show it. The v2
  `session.command({ sessionID, name, text })` client method exists, and
  `opencode api <operation>` can reach the server, but the command operation is
  undocumented and my attempts returned no effect. Task 4.1 needs either the
  exact server operation/route for a command, or a TUI-driven path, before the
  command half of the functional gate can run.
- **Everything else in 4.x is drivable** with the hook: the dropped bucket's
  absence, anti-drift over a later message, the stored session staying
  unchanged, and id parity (live ids equal export ids, already proved).
