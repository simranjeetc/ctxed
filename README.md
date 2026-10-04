# ctxed

Inspect and edit an agent's context window as a file: see every entry you are
about to send, and remove the ones you don't want.

`ctxed` reads a session document, shows each entry with its token cost, and
writes a copy with chosen entries removed. The input is never modified.

## Positioning: not another DCP

[OpenCode DCP](https://github.com/OpenCode-DCP/opencode-dynamic-context-pruning)
prunes automatically, inside the process, and hides the result — it swaps
summaries and placeholders into the outbound transcript and never touches stored
history. ctxed inverts the control model: it acts on a real file, shows you the
entries, and lets a human or a harness decide. It is the transparent,
steerable counterpart to an automatic pruner, not a faster one.

ctxed does not reduce tokens on its own. It removes only the entries you tell it
to — by index, or by high-level category. Compression (replacing a range with a
summary) and automatic pruning remain out of scope and are tracked in
[`docs/future-enhancements.md`](docs/future-enhancements.md).

## Two live flows, two different guarantees

Pruning a **running** session is offered on two harnesses, and they do **not**
promise the same thing. Read the difference before relying on either:

| | OpenCode — live bucket prune | Claude Code — compaction steering |
| --- | --- | --- |
| Entry point | `/ctxed-prune` inside the session | `ctxed-prune-context` **skill**, or `ctxed compact-instruction` in a terminal |
| Stays in session? | Yes — pick buckets, done | Yes with the skill (the agent runs the commands); the terminal flow leaves the session |
| What it does | Drops the named messages **by id** | Asks Claude's `/compact` to drop the named buckets |
| Guarantee | **Exact** — the named messages are gone | **Best-effort** — the summary is steered, not forced |

In Claude Code the recommended path is the **`ctxed-prune-context` skill**
([`.claude/skills/ctxed-prune-context/`](.claude/skills/ctxed-prune-context/)):
say *"drop the adapter topic"*, the agent finds the transcript, runs the
commands, and gives you the `/compact` sentence to paste. It needs
`categorize`'s model to be configured (see
[Model configuration](#model-configuration)) and it inherits the best-effort
ceiling — it automates the plumbing, not the guarantee.

If you need a dropped topic to be *provably* absent, use the OpenCode flow. The
Claude Code flow biases a summary toward your intent; it does not guarantee a
specific entry is removed. Both leave the **stored** session untouched.

**Why not just restart?** A restart throws away the whole thread. This drops one
topic and keeps the rest — the thread, the decisions, and the working context
survive. If that isn't worth it to you, a restart is simpler and you should use it.

## Build

```sh
go build -o ctxed ./cmd/ctxed
```

## Usage

```sh
ctxed inspect    <session> [--json] [--model M] [--tokenizer ENC]
ctxed drop       <session> --indices 3,7,9 [--out FILE] [--json] [--force] \
                 [--model M] [--tokenizer ENC]
ctxed categorize <session> [--model M] [--base-url URL] [--api-key K] \
                 [--categorizer-cmd CMD] [--max-categories N] [--out FILE]
ctxed prune      <session> (--categories-file F --categories 1,3 | --ids id1,id2)
                 [--ids-only]
ctxed compact-instruction <session> --categories-file F --categories 1,3
```

The session path may appear before or after the flags. A `<session>` of `-`
reads the transcript on stdin, so a plugin can categorize or prune the live
messages without exporting the session first:

```sh
$ opencode session export <id> | ctxed categorize - --model M --out cats.json
$ opencode session export <id> | ctxed prune - --categories-file cats.json --categories 1 --ids-only
```

A `--out` of `-` is the output counterpart: `categorize` prints the categories
document to stdout instead of writing a file, so a caller never manages a temp
path.

```sh
$ opencode session export <id> | ctxed categorize - --model M --out -
```

### inspect

```sh
$ ctxed inspect session.json
IDX  ROLE       KIND       TOKENS  PREVIEW
0    user       message    103     add retry to the upload client
1    assistant  tool-call  479     Let me look at the upload client first.
2    system     message    114     New skills are available…
TOTAL 3 entries · 696 tokens  (tokenizer: approximation · approximate)
```

`--json` emits the same fields as an object for a harness to consume.

### drop

```sh
$ ctxed drop session.json --indices 3,7,9
wrote session.edited.json
entries 62 → 59 · tokens 48,213 → 47,006 (-1,207)
```

Writes `<name>.edited.<ext>` by default; `--out` overrides. The input file is
left byte-for-byte unchanged. `--json` prints a stats object instead.

### categorize — what is this session about?

A model groups the entries into 2–5 high-level categories (five by default) and
ctxed writes an editable file plus a high-level table:

```sh
$ ctxed categorize session.json --model gpt-4o-mini
CAT  LABEL                        ENTRIES  TOKENS
1    Claude Code adapter work     38       96,120
2    Codex adapter investigation  12       28,400
wrote session.categories.json
```

Edit `session.categories.json` if the labels or membership are wrong — rename a
category, move an entry, or pull one out. `categorize` never drops anything.

### prune — apply a selection, non-destructively

> **Guarantee: exact.** The selected entries are removed by id; the named
> messages are gone from the outbound transcript.

`prune` emits the transcript with the selected entries excluded, on stdout. It
does not touch the session and does not create a new one:

```sh
$ ctxed prune session.json --categories-file session.categories.json --categories 1
# pruned transcript on stdout; the session file is never written
```

A harness plugin substitutes that transcript at dispatch (see
[`docs/plugin-contract.md`](docs/plugin-contract.md)). If a selection would
orphan a tool result, ctxed drops the dependent entry too and reports it on
stderr as an `adjustment:` line.

### compact-instruction — prune a live Claude Code session

> **Guarantee: best-effort.** This steers Claude's `/compact`; it does **not**
> force a specific entry to be removed the way the OpenCode flow does. Use it to
> bias a summary toward your intent, not to prove a topic is gone.

**Prefer the skill.** In a Claude Code session, the `ctxed-prune-context` skill
([`.claude/skills/ctxed-prune-context/`](.claude/skills/ctxed-prune-context/))
wraps the steps below: say *"drop the adapter topic"* and the agent finds the
transcript, runs `categorize`, shows you the buckets, and hands you the
`/compact` sentence. Use `compact-instruction` directly when you are driving by
hand from a terminal.

Claude Code cannot rewrite the outbound request from a hook, so its live prune
uses compaction instead. `compact-instruction` turns a category selection into a
single sentence naming the buckets to keep and drop, which you paste after
`/compact ` in the running session:

```sh
$ ctxed compact-instruction ~/.claude/projects/<project>/<session>.jsonl \
    --categories-file session.categories.json --categories 2

When you compact this session, keep the context about Context editor design discussion, and drop the context about Adapter implementation notes.
```

Then, in the same session:

```text
/compact When you compact this session, keep the context about … and drop the context about …
```

The instruction is deterministic, offline, and calls no model — it only reads
the categories file. Claude Code's own compaction rewrites history in place, so
the prune lands live, in the same session, with no relaunch and no file edit.
The retained detail is model-mediated: the instruction biases the summary, it
does not guarantee a specific entry survives. A future change adds a Claude Code
mod that drops the selected buckets exactly; see
`openspec/changes/add-claude-code-live-prune`.

### ids-only — the drop set for a live plugin

`--ids-only` prints just the resolved set of **dropped** entry ids — after orphan
resolution — as JSON, instead of a transcript. A plugin that holds live message
objects (OpenCode's dispatch hook) filters by id without re-serialising the
transcript on every dispatch:

```sh
$ ctxed prune session.json --categories-file session.categories.json --categories 1 --ids-only
{"droppedIds":["msg_101f9625c001NLzjIh2rzpuhNj"],"droppedToolCallIds":["call_00_…"]}
```

`droppedToolCallIds` names the tool-call ids the dropped entries issued or
answered, so a plugin can drop a tool result that lives in a message with no id
of its own. The stored session is still never written. See
[`plugin/opencode/`](plugin/opencode/) for the OpenCode plugin that consumes it.

### Model configuration

`categorize` reaches a model two ways:

```sh
ctxed categorize session.json --model gpt-4o \
    --base-url https://api.openai.com/v1 --api-key "$OPENAI_API_KEY"
ctxed categorize session.json --categorizer-cmd 'llm -m gpt-4o'  # prompt on stdin, response on stdout
```

For this machine there is a ready wrapper that needs **no API key** — it asks an
OpenCode Go model the machine is already entitled to:

```sh
ctxed categorize session.json \
    --categorizer-cmd "$PWD/scripts/ctxed-categorizer-opencode.sh"
# override the model with CTXED_CATEGORIZER_MODEL (default opencode-go/deepseek-v4-flash)
```

Environment fallbacks: `OPENAI_BASE_URL`, `OPENAI_API_KEY`, `CTXED_MODEL`. The
command override takes precedence and makes the whole path offline and
deterministic, so tests need no network — and it is the path for a provider
reachable only through a CLI.

### Choosing a harness

**Claude Code** — point ctxed at the transcript directly:

```sh
ctxed inspect ~/.claude/projects/<project>/<session>.jsonl
```

Claude Code's hooks cannot rewrite the outbound messages, so it cannot be
pruned at dispatch. Instead, categorize the transcript, pick the buckets to
drop, and turn that selection into a `/compact` instruction applied to the
running session:

```sh
ctxed categorize          ~/.claude/projects/<project>/<session>.jsonl --model gpt-4o
ctxed compact-instruction ~/.claude/projects/<project>/<session>.jsonl \
    --categories-file session.categories.json --categories 2
# then paste the printed sentence after `/compact ` in the session
```

**OpenCode** — sessions live in SQLite, so export first:

```sh
opencode session export <session-id> > session.json
ctxed inspect session.json
ctxed drop session.json --indices 3,7,9   # writes session.edited.json
```

ctxed never opens the database. Note: OpenCode cannot import an edited export
back into an existing session in place, so this produces a portable artifact.
Continuing a live session with pruned context is handled non-destructively at
dispatch (see the `add-category-prune` change). See
[`docs/adapters.md`](docs/adapters.md).

## Token counts

Counts come from the tokenizer of the model you name, when a real one is
available. Otherwise ctxed uses a documented approximation — one token per four
runes, rounded up — and labels every count `approximate` in the output. For
example, the 5-rune string `abcde` is counted as 2 tokens.

```sh
ctxed inspect session.json --model gpt-4o            # real BPE encoding
ctxed inspect session.json --tokenizer cl100k_base   # explicit encoding
```

## Structural safety

If dropping an entry would orphan a tool result — a result whose tool call is
gone — ctxed refuses to write and names the offending entries. An explicit
`--force` writes anyway and reports what it found. The default is always the
safe one for an artifact fed back into an agent loop.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | runtime error (missing file, unrecognized format, write failure) |
| 2 | usage error |
| 3 | refused (invalid indices, or an edit that orphans tool results) |

Data and stats go to stdout; diagnostics go to stderr. Write commands need no
terminal, so a harness can call them programmatically.

## Development

```sh
go test ./...                                   # offline, deterministic
CTXED_TEST_TIKTOKEN=1 go test ./internal/tokenize/   # exercise the real tokenizer
```

Test fixtures under `testdata/` are trimmed, schema-faithful samples of a real
OpenCode export and a real Claude Code transcript.
