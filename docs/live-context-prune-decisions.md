# Live context pruning — locked decisions

Status: **decided** (2026-10-04). This records the reasoning and evidence behind
how `ctxed` applies a prune to a *running* session, so neither harness brief nor
implementation re-derives it. Do not re-open these without a new decision entry.

## 1. The requirement (corrected, authoritative)

The goal is **not** to review or drop individual messages — that is not humanly
possible — and it is **not** to copy session files around or relaunch.

It is:

- **R1 — Category granularity.** See the context of the *current session* grouped
  into a small number of topic buckets (2–5). Decide which buckets to drop.
- **R2 — In-session, no relaunch, no hand-edited files.** The prune must land in
  the running session and the user continues in it.
- **R3 — Human picks buckets; the machine maps messages to buckets.** The human
  never maps a message to a bucket, and never inspects messages one by one.
- **R4 — History rewrite is acceptable.** Where a harness only offers a
  history-rewriting mechanism, that is allowed. It is not required.

Consequence: because the human decision is coarse (buckets) and the machine does
the classification, the stored-entry ↔ live-message correspondence problem that
plagued per-entry pruning largely disappears. The live mechanisms classify the
**live** message list, so indices/handles are exact.

## 2. Established facts (with evidence)

### 2.1 Claude Code — settings hooks cannot rewrite outbound

`UserPromptSubmit` "can't replace the prompt; it only injects `additionalContext`
alongside it". Same shape for `SessionStart`/`Stop`: they add context, never
remove messages. → a settings hook cannot prune the outbound transcript.
Source: <https://docs.claude.com/en/docs/claude-code/hooks>

### 2.2 Claude Code — mods exist but still cannot rewrite the request

Claude Code has in-process JS "mods" (early access, v2.1.287+; the machine runs
v2.1.288). They form a middleware chain over events. But the model-request event
`turn.step` pins the transcript:

> "The transcript is not on it: `messageCount` says how many messages the request
> carries, and `$.session.messages()` reads them. A hook rewrites `model` or
> `effort` going down; the rest is pinned."

A hook on `turn.step` may rewrite only `model`/`effort` going down and response
chunks coming up. → no dispatch-time prune via mods either.
Source: mods reference + the shipped `claude-code.d.ts` (v2.1.277/2.1.288).

### 2.3 Claude Code — compaction *is* a message-replacing, live surface

The only event that replaces the conversation is `session.compact`:

> "What the transcript becomes ... a summary and the kept messages in the
> transcript's place."

- A plugin can trigger it: `$.session.compact()` — "Compacts the conversation:
  the event `session.compact` with `trigger` `plugin`, the same call `/compact`
  makes, **between turns** ... resolves `{ skip }` when a hook vetoed it; rejects
  while a turn runs."
- `SessionCompactInput.messages` is writable: `next({ ...e, messages })` changes
  what is summarized. Each message carries an opaque `handle`; a message handed
  back **with** its handle is the engine's own, one **without** is read as built
  from `role`/`text`/tool blocks. A hook that answers without calling `next`
  supplies the final list (drop, and/or summary, deterministically).

→ Claude Code **can** be pruned live, at category granularity, via compaction.
History is rewritten in the process (R4 permits this).

### 2.4 Claude Code — manual `/compact [instructions]`

`/compact [instructions]` "Replace history with a summary, optionally focused on
what you specify." Free-text, live, same session, no code. Model-mediated: it
biases the summary, it does not guarantee a specific entry survives.
Source: <https://docs.claude.com/en/docs/claude-code/sessions>

### 2.5 Claude Code — the transcript is a log, not the live context

> "The transcript file is written asynchronously and may lag the in-memory
> conversation."

Editing the `.jsonl` mid-session does nothing to a running session, and the live
process keeps appending to it. Taking an edit into account requires exiting and
`claude --resume`/`-c`. → any file-edit approach is a relaunch. **Rejected**
(see §4).
Source: <https://docs.claude.com/en/docs/claude-code/hooks> (common input fields)

### 2.6 OpenCode — dispatch hook is the pruning seam

OpenCode exposes `session.hook("context", handler)` (the hook DCP uses), which
runs as a request is assembled and can change the outbound messages. Message ids
are stable (`msg_…`). This is the already-spec'd mechanism in
`openspec/changes/add-opencode-dispatch-plugin`. Source:
<https://opencode.ai/docs/plugins/>

### 2.7 OpenCode — compaction exposes instructions only, not the message list

`experimental.session.compacting` fires before the summary is generated and
exposes `output.context` (append guidance) and `output.prompt` (replace the
summarizer prompt). It does **not** expose the message list, so it cannot
deterministically drop chosen entries. → deterministic category drop in OpenCode
stays on the dispatch hook (§2.6), not on compaction.
Source: <https://opencode.ai/docs/plugins/>

### 2.8 Credentials and config dir (recorded for completeness)

`CLAUDE_CONFIG_DIR` relocates settings, session history, plugins **and**
credentials (macOS Keychain keyed per directory), so a temp config dir means
re-login. Relevant only to the rejected file-copy approach.

## 3. Decisions

- **D1 — Unit of pruning is the category bucket.** `categorize` computes buckets
  over the session's current content; the human selects buckets to drop; the
  machine drops every message assigned to a selected bucket.
- **D2 — OpenCode: apply the dispatch plugin.** Non-destructive, live, id-based.
  This is the existing change `add-opencode-dispatch-plugin`. No redesign.
- **D3 — Claude Code: use compaction, live.**
  - D3a (first, ships now): `ctxed` generates a `/compact` instruction from the
    selected buckets; the user runs `/compact <instruction>` in the session.
    Zero harness integration, live, same session. Model-mediated.
  - D3b (follow-up): a Claude Code mod whose `session.compact` hook classifies
    the live message list into buckets, drops the selected buckets, and returns
    the kept list — optionally summarising chosen buckets via `next`. Exact,
    live, history-rewriting. Early-access API.
- **D4 — Rejected Claude Code mechanisms (do not revisit).** Copy-transcript +
  `--resume`; in-place `.jsonl` rewrite + relaunch. Both are per-entry/file-based
  and both need a relaunch, violating R2. They survive only as an offline
  "edit a stored session" feature, not as the live path.
- **D5 — Proxy is a separate, future, harness-agnostic change.** `ANTHROPIC_BASE_URL`
  / OpenCode `provider.*.options.baseURL`, live and non-destructive, but larger
  (SSE relay, auth/beta forwarding, cache_control and preserved-thinking
  hazards) and its own online pruner. Tracked in `docs/future-enhancements.md`;
  not part of D2/D3.
- **D6 — Compression stays out of scope** except where a harness's compaction
  provides it (D3). `ctxed` itself still only drops.

## 4. Non-goals

- Per-message keep/drop as the primary UX.
- File copy, worktree, or relaunch as the way a prune is applied.
- Automatic/budget-triggered pruning.
- A proxy in this work.

## 5. Work split for subagents

**Agent A — OpenCode (implements D2).** Apply
`openspec/changes/add-opencode-dispatch-plugin` (13 tasks): `ctxed prune
--ids-only` (Go, additive, tested) and `plugin/opencode/` (thin TS, pinned
`@opencode-ai/plugin`), with the JS toolchain. Fail-open per the change's D3.
Do not touch Claude Code work.

**Agent B — Claude Code (implements D3a, specs D3b).** Add an OpenSpec change
`add-claude-code-live-prune` capturing §2.1–§2.5 and D3; implement the `ctxed`
side of D3a — a command that renders the `/compact` instruction text from a
categories file plus selected category ids (offline, deterministic, unit-tested,
reads nothing from the network). Leave D3b (the mod) as spec'd tasks, not built.

Both agents: a git worktree + branch off `main`, commit there, do not merge.
Branches: `feat/opencode-dispatch-plugin`, `feat/claude-code-live-prune`.

## 6. Open questions (verify at implementation time)

- D3b: does `$.session.compact()` install its result live, between turns, exactly
  as the docs state? Confirm with a real session before relying on it.
- D3b: does the hook run for the main conversation (not just subagents)?
- D3a: how much the `/compact` instruction actually steers retention in practice.
- OpenCode: exact field names in the `session.hook("context")` payload (already
  flagged in the change's design).
