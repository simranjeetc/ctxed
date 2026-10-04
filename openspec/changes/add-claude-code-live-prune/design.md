# Design

## Context

See `proposal.md` and the locked decisions in
`docs/live-context-prune-decisions.md` (facts §2.1–§2.5, decisions D3–D4). That
document is authoritative; this design only records how the change binds to it.
The facts are not re-investigated here.

The requirement (decisions §1) is category-level and in-session:

- **R1** — the current session's content is grouped into 2–5 topic buckets; the
  human decides which buckets to drop.
- **R2** — the prune lands in the running session; no relaunch, no hand-edited
  files.
- **R3** — the human picks buckets; the machine maps messages to buckets, never
  the reverse.
- **R4** — a history rewrite is acceptable where the harness only offers one.

For Claude Code, the only surface that both replaces messages and runs live is
compaction. Hooks cannot remove messages (§2.1); mods pin the transcript on
`turn.step` (§2.2); a `.jsonl` edit needs `--resume` (§2.5). So the change has
two stages (D3): a manual, model-mediated instruction now (D3a) and an exact mod
later (D3b).

## Goals / Non-Goals

**Goals:**

- Let a person prune a live Claude Code session at category granularity, in the
  session, without relaunching or editing files.
- Keep the shipped part offline, deterministic, and side-effect-free.
- Record the exact-drop mod (D3b) so it is not re-derived later.

**Non-Goals:**

- Per-message keep/drop as the primary UX.
- File copy, worktree, or relaunch as the way a prune is applied.
- Building the D3b mod in this change.
- Automatic or budget-triggered pruning.
- A proxy or request interceptor.

## Decisions

### D3a: Emit a `/compact` instruction, applied by the user

`ctxed compact-instruction <session> --categories-file F --categories 1,3`
prints one sentence naming the buckets to keep (the unselected categories) and
the buckets to drop (the selected ids), by label.

- **Why:** `/compact [instructions]` is free-text, live, same-session, and needs
  no code or hook (decisions §2.4). Naming the buckets is the whole decision the
  human made; ctxed only renders it.
- **Offline and deterministic:** reads the categories file (validated against
  the session), calls no model, writes no file, rewrites no transcript.
- **Reuses existing parsing:** the selection is resolved with
  `categorize.Select`, so an unknown category id is reported exactly as in
  `prune`.
- **Trade-off:** compaction is model-mediated — the instruction biases the
  summary, it does not guarantee a named bucket disappears (§2.4). That is
  acceptable because R2 forbids the alternatives that would be exact, and R4
  permits a history rewrite. D3b exists to make it exact.
- **Alternative considered:** a settings hook that rewrites the request.
  Rejected — hooks only add context (§2.1).

### D3b: A `session.compact` mod (specified, not built)

A Claude Code mod registers the `session.compact` hook, classifies the live
message list into the selected buckets, drops the selected ones, and returns the
kept list — optionally summarising through `next`.

- **Why:** `SessionCompactInput.messages` is writable, the hook can supply the
  final list, and `$.session.compact()` triggers the same compaction `/compact`
  makes, between turns (decisions §2.3). This is exact and live.
- **Status:** early-access API. Not built here; the tasks record it. The open
  questions in decisions §6 (does it install live; main conversation vs
  subagents) must be confirmed against a real session first.

### D4: Rejected mechanisms (do not revisit)

Copy-transcript + `--resume`, and in-place `.jsonl` rewrite + relaunch. Both are
per-entry/file-based and both need a relaunch, violating R2. They survive only
as an offline "edit a stored session" feature (`drop`), not as the live path.
Recorded here so neither the brief nor an implementation re-opens them.

## Risks / Trade-offs

- [The `/compact` instruction steers retention only weakly] → D3b is the exact
  path; the instruction is explicitly model-mediated, and the README says so.
- [The user forgets to paste or pastes a stale sentence] → the command is
  one line and re-runnable; it changes no state.
- [The categories file is edited between `categorize` and the command] →
  `categorize.Load` validates it against the session and rejects unknown ids.

## Migration Plan

Additive. A new package, one new subcommand, and documentation. Removing the
subcommand restores the prior state; no session, file, or store is modified by
D3a. D3b would be a separately removable mod.

## Open Questions

- D3b: does `$.session.compact()` install its result live, between turns, as the
  docs state? Does the hook run for the main conversation, not just subagents?
  (Decisions §6.) To confirm before relying on D3b.
- D3a: how much the instruction actually steers retention in practice.
