# Future enhancements

Parked scope from `openspec/changes/add-ctxed-cli`. That change ships **inspect** and
**drop** only. Everything below is deliberately deferred: useful, designed enough to
start from, but not part of the first change. Each item notes what it would take to
promote it to its own OpenSpec change.

> **Update:** non-destructive dispatch-time pruning (item 4) and model-assisted
> categorization have been promoted into `openspec/changes/add-category-prune`, and
> the model-transport pattern (item 2) is reused there for categorization. The items
> below remain future work unless noted.

## 1. Range compression with summaries

The core DCP primitive: replace a closed range of entries with one summary entry, so a
stretch of conversation costs one entry instead of many.

- Command: `ctxed compress <session> --range 12-40` (multiple disjoint ranges allowed;
  overlapping ranges rejected; ranges are inclusive and refer to original indices).
- The range is replaced by a single synthetic entry at the range's position, using a
  bracketed marker so the edit is identifiable and re-runnable, e.g.
  `[ctxed-compress 12-40] <summary text>`.
- Summary provenance resolves in a fixed precedence: an explicitly supplied summary
  (inline or file) → an override command → a built-in model endpoint. The chosen source
  is recorded in the result. Supplying a summary makes zero outbound requests.
- The summarizer only ever receives the targeted range. A range over a configured size
  limit errors rather than silently truncating.
- Extends the existing `context-pruning` capability; no redesign of the adapter or
  document model is needed. Structural-validity checking (a range must not split a
  tool call/result pair) already exists for drop and applies here.

## 2. Model-backed summarization

Two implementations behind one `Summarize(ctx, rangeText) (string, error)` interface:

- **OpenAI-compatible client**: HTTP POST to a chat-completions endpoint; base URL, API
  key, and model from flags or environment. Covers OpenCode's providers, local
  Ollama/LM Studio, and OpenAI itself with no vendor SDK.
- **Override command** (`--summarizer-cmd`): runs a caller-named command, writes the
  range to its stdin, reads the summary from stdout. Takes precedence over the endpoint.
  This is the path for harnesses whose models are reachable only through a CLI.

Tests must use a fixed-output command and a stub HTTP server; no test may reach a live
model. Adds a runtime network dependency, which is why it did not belong in the
inspect/drop foundation.

## 3. Automatic compression

Make compression happen without a human typing the range:

- `ctxed compress --auto --budget <tokens>`: a policy picks the ranges itself. First
  candidates are stale, superseded, and errored tool outputs; then the largest
  low-relevance spans; stop when under budget or when nothing is safely compressible.
- In-flight output mode: emit the transformed transcript on stdout (`--stdin` /
  `--transcript-out`) instead of writing a file, since a harness mid-turn does not want
  a file. Stored session history stays untouched, matching DCP's semantics.

## 4. Trigger surfaces

`--auto` still needs something to call it at dispatch time. Two options, in order of
increasing scope:

- **Per-harness hooks**: a thin OpenCode plugin (same dispatch hook DCP uses) and a
  Claude Code hook that shell out to `ctxed compress --auto` and substitute the result
  into the outbound request. Policy and summarization live in the one Go binary; the
  hook is just a caller.
- **Proxy mode** (Sleev-style): `ctxed run --budget N -- <agent>` sits in front of the
  model calls, rewrites each outbound body over budget, and forwards it. One
  integration covers every harness — the closest match to the "any famous harness"
  goal — but far larger than a CLI.

**Decision recorded when this was descoped:** automate via a per-harness hook first
(promoting `ctxed compress --auto` plus the OpenCode plugin), and consider the proxy as
a later change. This preserves the harness-agnostic format goal without committing to
a proxy rewrite.

## 5. Other deferred work

- **Interactive picker**: a TUI where you toggle keep/drop per entry with the keyboard,
  instead of passing `--indices`.
- **Human `--collapse`**: a range→summary replacement with a caller-supplied summary and
  no model call. This is just `compress --summary`; noted separately because it is the
  fully offline variant.
- **Additional adapters**: Codex rollouts (`~/.codex/sessions`, JSONL) and any other
  harness, using the adapter interface in `openspec/changes/add-ctxed-cli/specs/context-session-format/spec.md`.
- **Nested re-compression**: if a later compression overlaps an earlier summary block,
  fold the earlier summary into the new one so information survives layering rather than
  being diluted, as DCP does.
- **Python analysis repo**: aggregate token/pruning statistics across many saved
  sessions. Explicitly a separate repository, not this binary.

## Positioning to keep in mind

ctxed and DCP share the goal (shrink context) but invert the control model: DCP prunes
automatically, in-process, and hides the result; ctxed acts on a file and lets a human
inspect and override. Items 1–4 above are what close the automation gap — until they
land, ctxed does not reduce tokens on its own. The README must say this plainly so the
tool is not read as a worse DCP.
