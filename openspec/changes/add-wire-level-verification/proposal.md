# Proposal

## Why

The functional suite's hard evidence that a bucket was dropped is the plugin's
own debug log (`CTXED_PLUGIN_DEBUG_LOG`, read by `hook_outbound` in
`scripts/verify-functionally.sh`). The plugin is grading itself: if the hook
logs one list and OpenCode sends another (a later hook re-adds messages, the
mutation is not honored, a tool result travels in a message with no id), the
suite still passes. The remaining evidence is model recall ("do you remember
X?"), which is slow, costs model calls, and is non-deterministic.

For Claude Code there is no evidence at all of *what was sent* after `/compact`;
the suite can only see the transcript file.

The only oracle that matches the user's guarantee ("the dropped bucket is not in
what the model receives") is the outbound request itself.

## What Changes

- A small **recording fake provider** (`scripts/fakeprovider/`, Go, no deps)
  that speaks enough of the OpenAI-compatible chat API and the Anthropic
  Messages API to answer canned replies, and appends every request body to a
  JSONL log.
- The OpenCode scenarios point a custom provider at the fake server (OpenCode
  config `provider.<name>.options.baseURL`), so every dispatch is recorded.
- The Claude Code scenario points `ANTHROPIC_BASE_URL` at the fake server, so
  the `/compact` request and every following turn are recorded.
- Assertions move to the wire:
  - OpenCode: every dropped-bucket sentinel is absent from the next request
    body, every kept sentinel is present, and anti-drift holds on the wire.
  - Claude Code (D3a, best-effort): the compaction request contains the ctxed
    instruction; the first post-compact request no longer contains the
    pre-compact turns verbatim. Whether the dropped topic survives in the
    summary is reported as a **metric**, not a gate, because the fake provider
    writes the summary. When the D3b mod lands, the same harness asserts the
    exact drop.
- Model-recall checks become optional (`--with-model`), run against a real
  model as soft checks.
- The default functional run needs **no model entitlement and no network**; it
  needs only the harness binaries.

## Capabilities

### New Capabilities

- `wire-level-verification`: functional scenarios assert on the request the
  harness sends to the model, captured by a local recording provider.

## Impact

- New `scripts/fakeprovider/` (Go main package, built by the scripts).
- `scripts/verify-functionally.sh`: provider wiring and new assertions; the
  debug-log reads become secondary diagnostics.
- `docs/verification-strategy.md`: wire-level is the hard gate; prerequisites
  shrink (no OpenCode Go entitlement needed for the default run).
- Depends on `fix-verification-false-passes` (shared helpers, idle waiting).
