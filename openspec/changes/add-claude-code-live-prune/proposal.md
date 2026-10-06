# Proposal

## Why

ctxed can categorize a session and emit a pruned transcript, but for Claude Code
nothing applies the prune to a *running* session. This is not a gap in a hook we
can write around:

- **Settings hooks cannot rewrite outbound.** `UserPromptSubmit`,
  `SessionStart`, and `Stop` inject `additionalContext`; none remove messages
  (decisions §2.1).
- **Mods cannot rewrite the request either.** The model-request event
  `turn.step` exposes `messageCount` and `$.session.messages()`, but the
  transcript is pinned — a hook rewrites only `model`/`effort` going down
  (decisions §2.2).
- **Editing the transcript is a relaunch.** The `.jsonl` is written
  asynchronously and lags the in-memory conversation, so a mid-session edit does
  nothing until `claude --resume` (decisions §2.5).
- **Compaction is the one message-replacing live surface.** `session.compact`
  replaces the transcript with a summary plus the kept messages; `/compact
  [instructions]` is its manual, in-session form (decisions §2.3–§2.4).

The requirement is category-level and in-session: the human picks 2–5 topic
buckets to drop, the machine maps messages to buckets, and the prune lands in
the running session with no relaunch and no hand-edited files (decisions §1
R1–R4). Compaction satisfies it.

## What Changes

- **D3a (ships now).** A new ctxed command `compact-instruction` renders the
  selected buckets into one `/compact` instruction sentence that names the
  buckets to keep and the buckets to drop by label. The user pastes it after
  `/compact ` in the live session. It is offline and deterministic: it reads the
  categories file, calls no model, rewrites no transcript, and touches no file.
- **D3b dropped.** An exact-drop mod on the `session.compact` hook was
  specified and then dropped (2026-10-06): the `/compact` instruction is the
  Claude Code path, checked by a codeword test.
- **Documentation.** README usage and `docs/adapters.md` describe the flow:
  `categorize` → pick buckets → `ctxed compact-instruction …` →
  `/compact <text>`.

Deliberately **not** built: copy-transcript + `--resume`, and in-place `.jsonl`
rewrite + relaunch. Both are per-entry/file-based and both require a relaunch,
violating R2 (decisions D4).

## Capabilities

### New Capabilities

- `context-compact-instruction`: ctxed renders a deterministic, offline
  `/compact` instruction from a category selection, naming the kept and dropped
  buckets by label.
- `claude-code-live-prune`: the Claude Code live-prune flow — category-level,
  in-session, no relaunch — through the `/compact` instruction.

## Impact

- New: `internal/compact/` (Go, unit-tested) and the `compact-instruction`
  subcommand wired into `internal/cli`.
- Docs: README usage and harness flow; `docs/adapters.md` Claude Code note.
- No change to existing ctxed commands, adapters, or stored sessions. The
  command writes nothing and drops nothing.
