# Design

## Context

See `proposal.md` for motivation. This is a greenfield Go module; the repository is empty, so there is no existing code to integrate with.

**Prior art — OpenCode DCP.** Dynamic Context Pruning (`opencode-dcp`) hooks the model dispatch in-process, lets the model call a `compress` tool to pick ranges, substitutes model-written summaries and placeholders into the outbound transcript, and never touches stored history. ctxed borrows the goal but not the control model: it makes the message list inspectable and editable on disk. Compression — DCP's core primitive — is deliberately out of this change and captured in `docs/future-enhancements.md`. This design covers the foundation: read a session, show its entries, remove chosen entries, write the result back.

**Input shape constraint.** The two v1 harnesses do not store or hand off sessions the same way:

- OpenCode's source of truth is a SQLite database (`~/.local/share/opencode/opencode.db`). It is not a file ctxed reads. The adapter targets the JSON produced by `opencode session export <id>` — a document with an `info` object and a `messages` array. ctxed never opens the database. Note: `opencode session import` rejects an edited export of an existing session (`UNIQUE constraint failed: session_message.id`), so the edited document is a portable artifact, not an in-place write; returning a prune to a live OpenCode session is handled by dispatch-time pruning in `add-category-prune`.
- Claude Code transcripts are line-oriented JSONL files, one event object per line under `~/.claude/projects/<project>/<session>.jsonl`. The adapter reads and writes these directly.

So "a session file" cannot be assumed to be a single JSON object, and for OpenCode it is not a file at all until the harness exports one. The adapter boundary is bytes in, bytes out, and it must carry both layouts.

**Assumption (v1).** ctxed operates on session *documents*: a file the harness already keeps (Claude Code) or the output of the harness's own export command (OpenCode). It does not read a harness's live database or in-memory state, and it does not invoke the harness. The human or harness produces the document and ctxed writes an edited copy. Applying an edit back to a live session is out of scope for this change and is handled by dispatch-time pruning in `add-category-prune`; in particular, `opencode session import` cannot return an edited export to its own session in place. This keeps the mechanism hermetic and testable, and it is the only interpretation consistent with `ctxed inspect session.json`.

## Goals / Non-Goals

**Goals:**

- One canonical entry model that both a document-shaped (JSON) and a line-shaped (JSONL) harness can map onto without leaking either schema into inspection or pruning.
- Edits that preserve the source file's schema and unknown fields, so the written file is accepted by the harness that produced the input.
- Deterministic, offline tests: no network, no live harness.
- A drop path a harness can call programmatically, with a stable contract.

**Non-Goals:**

- Compression, summarization, and automatic pruning. Deferred; see `docs/future-enhancements.md`.
- Live harness plugins, hooks, or in-process integration. ctxed is a file-in, file-out CLI.
- Byte-for-byte identity of untouched regions. The contract is that the file re-parses and non-entry fields survive, not that formatting is preserved.
- Token-exact parity with every provider's tokenizer.

## Decisions

### D1: The adapter is a byte boundary, not a struct boundary

Each adapter implements three operations over raw bytes:

```
Detect(data []byte) bool
Parse(data []byte) (*Document, error)
Write(doc *Document) ([]byte, error)
```

`Document` carries the canonical ordered `[]Entry` plus the opaque source material needed to write back. Selecting the adapter is `Detect` over every registered adapter; the registry is the only list of supported formats.

- Why: it lets a JSON document and a JSONL stream share one interface without either shape contaminating the core.
- Alternative considered: one generic JSON walker that finds "the messages array". Rejected — it cannot answer Claude Code's line-oriented layout, and guessing which array is the message list is exactly the schema-coupling the adapter exists to avoid.

### D2: Preserve the source tree; mutate only targeted nodes

`Parse` keeps each entry's original serialized bytes (`json.RawMessage` for JSON, the original line for JSONL) and the wrapper: the top-level object with the entry array elided, or the surrounding non-event lines. `Write` clones the wrapper, removes only the entries an operation targets, and re-serializes.

- Why: this is how the "schema-preserving round-trip" and "unrelated fields survive" requirements are met without hand-modelling every harness field. ctxed never needs to understand a field it does not touch.
- Alternative considered: typed structs per harness. Rejected — every unknown field or new harness version becomes a data-loss bug.
- Trade-off accepted: key order and whitespace are not preserved; only structure and field values are.

### D3: Structural validity is checked from a call graph, and the write is refused by default

Before writing, ctxed builds the association between tool-call entries and tool-result entries from their ids. If a drop would leave a result whose call is gone, ctxed refuses and names the offending entries. An explicit force flag overrides, and the orphaning is reported.

- Why: a pruned session that a harness rejects is worse than a refused edit; silent corruption is the failure mode this tool exists to prevent.
- Alternative considered: warn-and-write by default. Rejected — the default must be the safe one for an artifact fed back into an agent loop.

### D4: Token counting is model-aware when possible, approximate and labelled otherwise

When the configured model maps to a known tokenizer encoding, ctxed uses it. Otherwise it uses a documented approximation and labels every count as approximate. A tokenizer override is available.

- Why: exactness matters for the keep/drop decision, but a hard dependency on every provider's tokenizer is not worth it for a small tool.
- Alternative considered: chars/4 only. Rejected — it is fine as the fallback but wrong as the only mode for the common OpenAI-compatible case, where a real encoding is available.

### D5: A harness-invocable CLI contract is part of the design, not an afterthought

Commands: `inspect`, `drop`. Stable flags: `--out`, `--json`, `--force`, `--tokenizer`, `--indices`, plus the read-only input-path argument. Data and stats go to stdout; diagnostics go to stderr. Exit codes: `0` success, `1` runtime error, `2` usage error, `3` refused (invalid or unsafe edit). Every write command emits a JSON stats object containing entries and tokens before and after and the removed indices.

- Why: making the invocation and the result machine-consumable is what lets a future harness integration — or the deferred compression work — call ctxed without changing the core contract.
- Alternative considered: human-only output. Rejected — it would make any harness path a rewrite later.

### D6: Edits are applied against original indices

All indices refer to the input session's numbering. Duplicates and out-of-range values are rejected before any work, and the output order is computed deterministically from the original order.

- Why: predictable and scriptable; a caller can compute indices once and trust them.

## Risks / Trade-offs

- [The real on-disk layout of a harness differs from the assumed export/transcript shape] → Each adapter is developed against a captured real sample committed as a test fixture; the schema is pinned in an adapter test, and the capability specs are layout-agnostic, so a layout correction changes the adapter, not the design.
- [Schema-preserving write-back still normalizes formatting] → Stated as a non-goal; the contract is re-parseability and field survival, verified by round-trip tests.
- [Approximate token counts mislead the keep/drop decision] → Counts are labelled approximate whenever they are not from a known tokenizer, and the output states which mode produced them.
- [Refusing on detected orphaning could block a legitimate edit] → An explicit force flag writes and reports; the refusal is information, not a wall.
- [ctxed on its own does not reduce tokens — it only removes entries a caller chooses, so it is not automatic like DCP] → This is a deliberate scope boundary, not an oversight. The deferred compression and automation work is captured in `docs/future-enhancements.md`, and it extends the `context-pruning` capability without a redesign.

## Migration Plan

New project: there is no existing system to migrate and no data to convert.

- Rollback is deleting the module and its binary; because input session files are never mutated, no user data is at risk from that rollback or from a bug in ctxed.
- The first change is additive by construction: it introduces the `ctxed` binary and three new capability specs. No existing spec or behavior is modified, so nothing downstream becomes incomparable.

## Open Questions

- The exact approximation formula for the no-tokenizer case is a documentation detail to fix during implementation; it does not change any spec, approach, or task.
- Compression, automatic pruning, harness hooks, a proxy mode, and an interactive picker are tracked in `docs/future-enhancements.md`. They are future work, not decisions this change must make.
