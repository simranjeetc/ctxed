# Command-line reference

`ctxed` is a single binary with four commands. Most people only ever need
`overview` (via the [`ctxed-overview`](../skills/ctxed-overview/SKILL.md) skill);
the rest are here for scripts and other harnesses.

```
ctxed overview [<session>] [--session ID] [--json] [--categorizer-cmd CMD]
              [--max-categories N] [--max-input-bytes N] [--no-model]
              [--model M] [--tokenizer ENC]
ctxed inspect <session> [--json] [--model M] [--tokenizer ENC]
ctxed categorize <session> [--model M] [--tokenizer ENC] [--base-url URL] [--api-key K]
                 [--categorizer-cmd CMD] [--max-categories N]
                 [--max-input-bytes N] [--out FILE]
ctxed update [--check] [--force]
ctxed skill install
```

`<session>` is a Claude Code transcript (`.jsonl`) or an OpenCode export
(`opencode session export <id> > s.json`).

## Session resolution

`overview` picks the session in this order:

1. the `<session>` argument,
2. `--session` (a Claude Code UUID or an OpenCode `ses_…` id),
3. the session the command runs in — `CLAUDE_CODE_SESSION_ID` →
   `~/.claude/projects/*/<id>.jsonl`, `OPENCODE_SESSION_ID` →
   `opencode session export <id>`.

If both harness variables are set it refuses and asks for `--session`.

## overview

Topics in the live context, each with messages, tokens, share and status, plus
what is still pending. Read-only.

| Flag | Meaning |
| --- | --- |
| `--session ID` | session id when no file argument is given |
| `--json` | emit the report as JSON |
| `--categorizer-cmd CMD` | shell command that names the topics (prompt on stdin, answer on stdout) |
| `--max-categories N` | maximum topics (default 5) |
| `--max-input-bytes N` | target prompt size; the session is sampled to fit (default 30000) |
| `--no-model` | skip the categorizer; print the whole-session sizes as one row, sending nothing to a model |
| `--model M`, `--tokenizer ENC` | use a real tokenizer instead of the estimate |

Every live entry lands in exactly one row, and the row totals sum to the session
total. If the model call fails, the sizes still print as one row and the error
goes to stderr.

## inspect

Lists the live context entry by entry: index, role, kind, tokens, preview.

```sh
ctxed inspect session.json
IDX  ROLE       KIND       TOKENS  PREVIEW
0    user       message    103     add retry to the upload client
```

`--json` emits the same fields as an object. `--model`/`--tokenizer` select the
tokenizer.

## categorize

Groups the entries into 2–5 categories and writes an editable
`<name>.categories.json`.

| Flag | Meaning |
| --- | --- |
| `--model M` | model name for the call and its tokenizer |
| `--base-url URL`, `--api-key K` | OpenAI-compatible endpoint (fallbacks `OPENAI_BASE_URL`, `OPENAI_API_KEY`) |
| `--categorizer-cmd CMD` | shell command instead of the HTTP endpoint |
| `--max-categories N` | maximum categories (default 5) |
| `--max-input-bytes N` | target prompt size (default 30000) |
| `--out FILE` | output path (default `<name>.categories.json`) |

`<session>` may be `-` to read a transcript from stdin.

## update

Replaces the running binary with the latest release, verified against the
release's `checksums.txt`, then refreshes the installed `ctxed-overview` skill
to match.

| Flag | Meaning |
| --- | --- |
| `--check` | report whether a newer release exists, without installing |
| `--force` | reinstall the latest release even if it is not newer |

## skill

```sh
ctxed skill install
```

Writes the `ctxed-overview` skill embedded in this binary into every agent
harness it finds (Claude Code at `~/.claude`, OpenCode at `~/.config/opencode`).
`ctxed update` runs this for you after replacing the binary.

## Environment

| Variable | Used by |
| --- | --- |
| `CTXED_CATEGORIZER_CMD` | `overview`, `categorize` — categorizer command |
| `CTXED_CATEGORIZER_MODEL` | `overview` — model for the harness's own CLI |
| `CTXED_CLAUDE_BIN`, `CTXED_OPENCODE_BIN` | `overview` — path to the harness CLI |
| `CTXED_MODEL` | `categorize` — model name fallback |
| `OPENAI_BASE_URL`, `OPENAI_API_KEY` | `categorize` — endpoint fallback |
| `CLAUDE_CODE_SESSION_ID`, `OPENCODE_SESSION_ID` | `overview` — session in use |

## Token counts

Counts are estimates by default (one token per four runes, labelled
`estimate`/`approximate`). Name a model or encoding for a real tokenizer:

```sh
ctxed inspect session.json --model gpt-4o
ctxed overview --tokenizer cl100k_base
```

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | success (also when only the topic names were unavailable) |
| 1 | runtime error (missing file or transcript, unrecognized format) |
| 2 | usage error (no session, ambiguous session, unsafe session id) |
