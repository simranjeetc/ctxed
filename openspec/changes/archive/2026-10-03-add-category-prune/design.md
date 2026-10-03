# Design

## Context

See `proposal.md` for motivation. This builds on `add-ctxed-cli`, which already
gives ctxed a canonical entry model, adapters for OpenCode and Claude Code, and a
file-based `inspect`/`drop`. The new work is a lens over the same entry list
(categorization) and a second way to apply a selection (at dispatch).

**Prior art — DCP.** Dynamic Context Pruning hooks the harness, lets the model
choose what to compress, and substitutes the result into the outbound transcript
on every dispatch, never touching stored history. This change keeps the
dispatch-time, non-destructive mechanism but moves the decision to a human: the
model proposes what the session is *about*, the person adjusts and selects, and
ctxed emits the pruned transcript.

**The hard constraint.** At dispatch the harness assembles its own outbound
message list, which need not enumerate entries the same way the stored session
does. A decision made against the stored session can only be applied reliably if
it references **stable ids** — OpenCode's `msg_…`, Claude's `uuid` — not
positions.

## Goals / Non-Goals

**Goals:**

- Turn a long session into a few labeled, costed categories a person can grasp.
- Let a person adjust membership and choose categories to drop; never auto-apply.
- Apply the prune at dispatch without modifying stored history or changing the
  session id.
- Keep all categorization and pruning logic in ctxed; a harness integration is a
  thin caller.

**Non-Goals:**

- Mutating a harness's session store.
- Compression or summarization of ranges.
- Unattended, budget-triggered pruning.
- Shipping a concrete harness plugin (next change).
- Treating unstable model output as authoritative.

## Decisions

### D1: Prune at dispatch, non-destructively — do not mutate the store

ctxed emits a pruned transcript for the request; the stored session is never
touched.

- Why: the user wants to continue the same session in place; deleting rows from
  OpenCode's live SQLite (undocumented v2 schema, held open by the server) is
  irreversible and version-coupled. Dispatch substitution gets the same effect
  with no downside to stored data and matches DCP's proven mechanism.
- Alternative considered: edit `session_message` rows directly. Rejected as the
  primary path; it breaks the "input never mutated" property that makes the tool
  safe.

### D2: Prune references are stable ids, not indices

Category membership and explicit selections are keyed by the harness's stable
message id, with a deterministic fallback id for entries that lack one.

- Why: the outbound list at dispatch may not match the stored enumeration; ids
  survive reordering and appends. This is exactly why DCP injects its own ids.
- Alternative considered: positional indices. Rejected: a new turn shifts them.

### D3: The model proposes; the person disposes

`categorize` produces a proposal and never drops. Dropping requires an explicit
selection in a separate `prune` invocation.

- Why: pruning removes information irreversibly for the rest of the session; a
  human gate is the point of the tool. The whole complaint about DCP is that its
  decisions are automatic and opaque.
- Alternative considered: auto-prune when over budget. Deferred; it contradicts
  the "steerable" purpose.

### D4: Categories are an editable sidecar file

`categorize` writes a machine-readable file (labels + id membership + token
totals); a person edits it and passes it back to `prune`.

- Why: it satisfies "adjust" without building a TUI, it is diffable and
  scriptable, and it is the natural input to the dispatch hook. Reading it back
  is validated: unknown ids are rejected rather than silently dropped.
- Alternative considered: an interactive picker. Deferred to
  `docs/future-enhancements.md`.

### D5: Model transport is the parked endpoint-plus-command pattern

An OpenAI-compatible endpoint by default, with a `--categorizer-cmd` override
that takes precedence. The command path makes tests offline and deterministic.

- Why: it reuses the transport already sketched in `docs/future-enhancements.md`
  and covers CLI-only providers. No SDK; one HTTP call.
- Alternative considered: a vendor SDK. Rejected: does not cover local or
  CLI-only models.

### D6: All logic in ctxed; the plugin is a thin caller

ctxed owns categorization, selection, id resolution, and validity handling. A
harness plugin only calls ctxed with its session and prune set and substitutes
the returned transcript. The exact inputs and outputs are documented in
`docs/plugin-contract.md`.

- Why: the user asked that the bulk live in ctxed so one utility serves every
  harness and each plugin stays minimal; it also keeps the harness-facing surface
  testable in Go.
- Alternative considered: per-harness logic in each plugin. Rejected: duplicates
  categorization and prune policy N times.

### D7: The emitted transcript is validated for structural integrity

Before emitting, ctxed applies the same tool-call/result check used by `drop`: a
result whose call is excluded is either excluded too or its pair retained, and
the adjustment is reported.

- Why: a transcript that orphans a tool result is invalid for the model and the
  harness; the prune must not produce one silently.
- Alternative considered: emit as-is and let the harness fail. Rejected.

### D8: Entries without a native id get a deterministic fallback id

A fallback id derived from stable entry content (not its position) is assigned
and used consistently, so membership survives re-enumeration.

- Why: not every entry type carries an id in every harness.
- Trade-off: two identical entries could collide; the fallback includes a
  disambiguating counter scoped to content, and read-back validation catches
  ambiguity.

## Risks / Trade-offs

- [Model categories are unstable or wrong] → Categorization is a proposal; a
  human adjusts membership by id before anything is pruned. Tests use a
  fixed-output command, so the pipeline is deterministic.
- [A long session exceeds the model's context] → Only the material needed is
  sent, bounded by a configured limit that errors rather than truncates.
- [Stable ids differ across harness versions or storage revisions] → References
  are validated on read-back; an unknown id is a hard error naming it, not a
  silent drop.
- [Plugin APIs churn] → The ctxed↔plugin contract is deliberately minimal
  (session in, pruned transcript out) and documented, so a plugin is small and
  cheap to update.
- [Categorizing a huge session is slow or costly] → Invocation is explicit and
  human-triggered; the command override enables a free local or cached run.
- [Dispatch pruning means the dropped entries still exist in storage] → This is
  the intended, non-destructive semantics: they are never sent, but nothing is
  lost. Stated plainly in the README.

## Migration Plan

Additive: new `categorize` and `prune` subcommands and new internal packages;
existing `inspect`/`drop` behavior and specs are unchanged. Rollback is removing
the new commands. Because nothing in a harness store is modified, there is no
data migration and no destructive failure mode.

## Open Questions

- Whether categories should be cached against a session revision is deferrable;
  the first version recomputes on demand.
- The default category maximum (five) and the exact fallback-id format are
  implementation details that can change without touching the specs.
