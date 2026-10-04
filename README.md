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
```

The session path may appear before or after the flags.

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

`--ids-only` prints just the resolved set of **dropped** entry ids — after orphan
resolution — as JSON, instead of a transcript. A plugin that holds live message
objects (OpenCode's dispatch hook) filters by id without re-serialising the
transcript on every dispatch:

```sh
$ ctxed prune session.json --categories-file session.categories.json --categories 1 --ids-only
{"droppedIds":["msg_101f9625c001NLzjIh2rzpuhNj"]}
```

The stored session is still never written. See
[`plugin/opencode/`](plugin/opencode/) for the OpenCode plugin that consumes it.

### Model configuration

`categorize` reaches a model two ways:

```sh
ctxed categorize session.json --model gpt-4o \
    --base-url https://api.openai.com/v1 --api-key "$OPENAI_API_KEY"
ctxed categorize session.json --categorizer-cmd 'llm -m gpt-4o'  # prompt on stdin, response on stdout
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
