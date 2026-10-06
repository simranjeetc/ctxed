---
name: ctxed-prune-context
description: Claude Code only; in OpenCode use the ctxed-prune skill instead. Drop a topic bucket from the current Claude Code session's context without restarting it. Use when the user says "prune context", "drop this topic", "context is too heavy", "free up context", "forget the <topic> discussion", or asks to reduce what the model is carrying.
allowed-tools: Bash(ctxed:*), Bash(ls:*), Bash(cat:*), Bash(command:*)
license: MIT
compatibility: Requires the ctxed CLI on PATH and a Claude Code session transcript.
metadata:
  author: ctxed
  version: "1.0"
---

# Prune session context

Steer Claude Code's own compaction so a chosen topic drops out of the running
session. The user picks a **topic bucket**; this skill turns that pick into the
one sentence `/compact` needs. The model is never asked to review individual
messages.

## Hard limits — read before promising anything

- **Best-effort, not exact.** Claude Code cannot rewrite the outbound request
  from a hook or a mod; the only live message-replacing surface is `/compact`.
  The sentence you produce **steers** the summary. It does not guarantee a
  specific message is removed. Never tell the user a topic is *provably* gone.
- **`/compact` runs between turns, and only the user can type it.** You cannot
  invoke compaction from inside a turn. Your job ends by handing the user a
  sentence to paste.
- **The transcript file lags the live conversation.** `categorize` reads the
  `.jsonl`, which is written asynchronously. The buckets may reflect the session
  as of the last flush, not the current turn. Say so if the user pruned right
  after a burst of activity.
- **Never edit the transcript.** Read it only.

## Resolve the ctxed binary first

`ctxed` must be runnable. If `command -v ctxed` finds nothing, this repo's own
build is the fallback:

```sh
ctxed --version || ctxed_bin="$(git rev-parse --show-toplevel)/ctxed"
# if neither works, build once: go build -o "$(git rev-parse --show-toplevel)/ctxed" ./cmd/ctxed
```

Use the resolved path in every command below. Never continue if ctxed cannot
run — report the missing prerequisite instead.

## Steps

1. **Find the current transcript.** The project directory is the working
   directory with every `/` replaced by `-`, under `~/.claude/projects/`. Map it
   directly and take the newest `.jsonl` in it:

   ```sh
   proj=~/.claude/projects/${PWD//\//-}
   ls -t "$proj"/*.jsonl | head -1
   ```

   If that directory has no `.jsonl`, or several are plausible, ask the user
   which session — do not guess and do not fall back to another project's
   transcript.

2. **Categorize.** This calls a model to group the entries into 2–5 topic
   buckets and writes an editable categories file:

   ```sh
   ctxed categorize <transcript> --out /tmp/ctxed-cats.json \
     --categorizer-cmd "${CLAUDE_PROJECT_DIR}/scripts/ctxed-categorizer-opencode.sh"
   ```

   `--out -` prints the categories JSON to stdout instead of writing a file.

   Only the **live context** is categorized. If the session was compacted
   before, ctxed starts at the last compaction: the earlier summary is one
   entry, followed by what came after. Topics that compaction already removed
   cannot come back as buckets, so never offer to drop them.

   That script asks an **OpenCode Go** model (no API key needed); override it
   with `CTXED_CATEGORIZER_MODEL`. `categorize` also accepts a direct model:

   ```sh
   ctxed categorize <transcript> --model M --base-url URL --api-key KEY
   # or the env fallbacks: OPENAI_BASE_URL, OPENAI_API_KEY, CTXED_MODEL
   ```

   If none is configured, ctxed exits with
   `ctxed: no model configured (missing base URL, API key, model)`.
   **When that happens, stop and tell the user** — do not invent buckets, do not
   hand-classify the messages, and do not proceed. The whole point is that
   ctxed, not the model, maps messages to buckets; improvising defeats it. Offer
   to run again once a model is configured, and point at
   `README.md § Model configuration`.

3. **Show the buckets and ask which to drop.** Read the categories file — each
   entry holds an `id`, a `label`, an `entryCount`, and a `tokens` figure:

   ```sh
   cat /tmp/ctxed-cats.json
   ```

   Present the labels with their size, so a wrong or stale pick is visible
   *before* the irreversible step, e.g.:

   ```
   1  Model selection command          1 entry   33 tokens
   2  Local command execution context  1 entry   55 tokens
   ```

   Category ids are as printed (1-based in a real file). Never show the raw
   message ids — the user picks topics, not messages. Ask exactly once which
   buckets to drop; accept multiple.

4. **Render the instruction.** The category ids are the pick from step 3:

   ```sh
   ctxed compact-instruction <transcript> \
     --categories-file /tmp/ctxed-cats.json --categories 2
   ```

   - **stdout** is the pasteable sentence, e.g.
     `When you compact this session, keep the context about … and drop … .`
   - **stderr** carries a caution line. Do not paste the caution.

5. **Hand it over, with the caveat.** Give the user the sentence in a fenced
   block, then tell them to paste it in the session as:

   ```text
   /compact <the sentence>
   ```

   Repeat that this steers the summary and does not guarantee removal — and
   that the stored session history is not modified by ctxed.

## What NOT to do

- Do not claim parity with OpenCode's flow. There, the plugin drops messages by
  id and the result is exact. Here it is model-mediated.
- Do not run `/compact` yourself, or claim you did.
- Do not summarize or drop messages by hand — that is what ctxed is for.
- Do not modify or rewrite the session file.
