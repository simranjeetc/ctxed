# Proposal

## Why

Agent context windows are managed by automatic, opaque heuristics. The harness compacts, or a plugin swaps model-written summaries into the outbound transcript, and you cannot see or overrule any of it. When the next turn goes wrong there is no way to answer "which entry caused this?" or "remove just that one".

ctxed makes the actual message list inspectable and editable. This change builds that foundation: see every entry and its token cost, and remove the entries you do not want. It is the transparent counterpart to OpenCode's Dynamic Context Pruning (DCP) — DCP prunes automatically, in-process, invisibly; ctxed shows the entries and lets a human or a harness act on them, as a real file on disk.

Compression (replacing a range with a summary) and automatic pruning are useful extensions, but they are deliberately **not** in this change. They are captured, with their design, in `docs/future-enhancements.md`. Shipping a complete, narrow inspect-and-remove tool reads as finished; bolting an unfinished compression path onto it does not.

## What Changes

- New Go CLI `ctxed`, shipped as one binary.
- `ctxed inspect <session>`: read-only table of entries — index, role, kind, token count, first-line preview — plus totals. Nothing is written.
- `ctxed drop <session> --indices 3,7,9`: writes an edited session with the listed entries removed. The input file is never modified.
- Session adapters behind one interface. v1 ships OpenCode and Claude Code. An edited session is written back in its source harness's own schema, so it can be handed to that harness unchanged.
- Token accounting per entry and in total, from the configured tokenizer or a documented, labelled approximation.
- Every write command is non-interactive and emits a JSON stats object (entries in/out, tokens before/after, removed indices) with stable exit codes, so an agent harness can call ctxed exactly where it would call a pruning hook.

Deferred to `docs/future-enhancements.md`, explicitly not built here: range compression with summaries, model-backed summarization, automatic budget-driven pruning, harness hooks, a proxy/in-flight mode, and an interactive picker.

## Capabilities

### New Capabilities

- `context-session-format`: detect, parse, normalize, and round-trip session files from supported agent harnesses behind a single adapter interface, preserving the source schema when writing an edited session.
- `context-inspection`: read-only inspection of a session as a list of addressable entries with per-entry token accounting and previews.
- `context-pruning`: remove entries from a session and write the reduced session, reporting token deltas in a form both humans and harnesses can consume. (Compression will extend this capability later.)

### Modified Capabilities

_None. This is a greenfield project with no existing specs._

## Impact

- New Go module and single `ctxed` binary: `cmd/ctxed`; internal packages for session model, adapters, inspection, and pruning; per-adapter test fixtures.
- No external network dependency. ctxed reads and writes local files only.
- `docs/future-enhancements.md` is added to record the deferred compression and automation work.
- No existing code, APIs, or systems are affected; the repository is empty.
- Out of scope: range compression and summarization, automatic or scheduled pruning, harness plugins/hooks, a proxy mode, an interactive picker, and adapters beyond OpenCode and Claude Code.
