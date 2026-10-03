# Design

## Context

See `proposal.md`. ctxed already emits a pruned transcript and documents the
plugin role in `docs/plugin-contract.md`. The mechanism this change binds to is
OpenCode's plugin API: the binary exposes `session.hook("context", handler)`,
which runs as a request is assembled — the same hook DCP uses. Claude Code's
hooks are command hooks keyed on events such as `SessionStart`; none can rewrite
the outbound message list, so Claude Code is out of scope here.

**The constraint that shapes the plugin.** At dispatch the plugin holds live
message objects; ctxed works on a session *document*. Handing the live transcript
to ctxed every dispatch would mean serialising and re-parsing it each time. Since
OpenCode message ids are stable (`msg_…`), the plugin instead asks ctxed once for
the set of **dropped ids** and filters live messages by id.

## Goals / Non-Goals

**Goals:**

- Apply ctxed's prune to every dispatch, non-destructively.
- Keep the plugin trivial: exec, parse, filter — no policy.
- Never let a ctxed problem break a turn (fail-open).
- Bounded per-dispatch cost.

**Non-Goals:**

- Claude Code (its hook surface cannot transform messages).
- Any automatic or budget-triggered pruning policy — the selection stays the
  user's explicit choice.
- Mutating stored session history.
- A proxy or request interceptor.

## Decisions

### D1: Bind to `session.hook("context")`

- Why: it is the dispatch-time transcript hook — the only place a plugin can
  change what is sent without sitting in front of the API.
- Alternative considered: an HTTP proxy. Rejected — vastly larger and
  unnecessary when the hook exists.

### D2: Ask ctxed for the dropped id set; filter by id

The plugin calls `ctxed prune … --ids-only` and removes messages whose id is in
`droppedIds`.

- Why: ids are stable and already present on messages; the plugin never
  serialises the transcript, so it stays thin and cheap.
- Alternative considered: pass the live transcript to ctxed (add a stdin mode)
  and substitute the returned transcript. Rejected — per-dispatch
  serialise/parse cost and a larger ctxed surface.
- Alternative considered: keep-only-kept-ids from the emitted transcript.
  Rejected — it cannot distinguish a dropped message from one that was never in
  the stale export, so it would drop new messages.

### D3: Fail-open

On any ctxed failure — non-zero exit, timeout, unparseable output — the plugin
sends the transcript unchanged and reports the error.

- Why: pruning is an optimization; it must never break the user's turn or block
  on a broken binary or stale file.
- Alternative considered: fail-closed. Rejected.

### D4: Cache the dropped set, refresh on change

The plugin caches the dropped set keyed by the selection and the session
revision, and re-invokes ctxed only when either changes.

- Why: `session.hook("context")` may run on every dispatch; shelling out each
  time is avoidable latency.
- Alternative considered: invoke on every dispatch. Rejected.

### D5: Configuration is a sidecar selection

The plugin reads the ctxed binary path (or `PATH`), the session export path, and
the categories file plus selected category ids or explicit ids, from config/env.

- Why: the selection is an explicit human decision recorded in the categories
  file, not live state; the plugin only reads it.
- Alternative considered: let the plugin infer categories. Rejected — that is
  policy, and it belongs in ctxed.

### D6: A small additive ctxed flag, `prune --ids-only`

ctxed prints a JSON object with `droppedIds` (the resolved set, after orphan
resolution) instead of a transcript.

- Why: the plugin needs the dropped set to filter by id; the emitted transcript
  only exposes the kept set.
- Alternative considered: compute the set in the plugin. Rejected — logic in the
  plugin.

### D7: No logic in the plugin

The plugin's only behavior is: run ctxed, parse `droppedIds`, remove matching
messages, and fail-open. A test asserts no category or validity logic is present.

## Risks / Trade-offs

- [OpenCode's plugin API is beta and may change] → the plugin is one hook and
  pins the API version; churn is a small edit.
- [The session export can be stale relative to live messages] → intentional:
  the dropped set covers the historical entries the user selected; messages
  created after the export are not in the set and are kept. Refreshing the
  export is part of re-running `categorize`.
- [Hook message ids might differ from export ids] → verify with a real dispatch
  during implementation; both surfaces use `msg_…` ids.
- [Shelling out per dispatch adds latency] → cache with refresh-on-change.
- [ctxed not installed or not on PATH] → the binary path is configurable, and a
  missing binary is a fail-open no-op with a reported error.

## Migration Plan

Additive. A new `plugin/opencode/` directory and one new ctxed flag; removing the
plugin restores default OpenCode behavior. Nothing in a session store is ever
modified, so there is no data migration and no destructive failure mode.

## Open Questions

- The exact field names in the context hook's payload need to be pinned against a
  real dispatch during implementation; this does not change any requirement.
