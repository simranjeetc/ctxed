# ctxed

See what an agent session's context is made of: which topics it holds, how many
tokens each takes, which are done, and what is still pending. Works the same in
Claude Code and OpenCode. Read-only: ctxed never changes a session.

```
$ ctxed overview
Session ses_ef89… · live context: 14 messages · ~86.4k tokens (estimate)

 #   Topic                                         Msgs   Tokens  Share  Status
 1   Direct API publish instead of GitHub Action      4    11.2k    13%  in progress
 2   Digest URL publish-chain investigation           5     4.6k     5%  done
 3   Pending steps recap                              4     2.3k     3%  done
     Compaction summary + kept tail                   1    68.2k    79%  —

Pending: digest_url still unset; Move email (send_digest) ahead of dossiers

Not counted: 690 messages from before the last compaction.
```

You decide what to do with it: carry on, compact, or start a new session.

## Inside a session: the `ctxed-overview` skill

One skill, [`skills/ctxed-overview/`](skills/ctxed-overview/), for both
harnesses. Ask "what's in my context?" (or `/ctxed-overview`); the agent runs
`ctxed overview` and shows the table as printed.

```
 Claude Code                    OpenCode
 skill: ctxed-overview          skill: ctxed-overview  (same file)
        │                              │
        └──────── ctxed overview ──────┘
                       │
   find session   CLAUDE_CODE_SESSION_ID → ~/.claude/projects/*/<id>.jsonl
                  OPENCODE_SESSION_ID    → opencode session export <id>
   live context   only what follows the last compaction, plus the compaction
   topics         one cheap model call: claude -p (Haiku) / opencode run
   print          table, or --json
```

There is no plugin and nothing in the request path.

### Install

```sh
go install ./cmd/ctxed                                     # ~/go/bin/ctxed
ln -s "$PWD/skills/ctxed-overview" ~/.claude/skills/        # Claude Code
mkdir -p ~/.config/opencode/skills/ctxed-overview \
  && cp skills/ctxed-overview/SKILL.md ~/.config/opencode/skills/ctxed-overview/   # OpenCode
```

Restart OpenCode's server afterwards; it loads skills at start.

## Usage

```
ctxed overview [<session>] [--session ID] [--json] [--categorizer-cmd CMD] [--max-categories N]
ctxed inspect <session> [--json] [--model M] [--tokenizer ENC]
ctxed categorize <session> [--model M] [--base-url URL] [--api-key K]
                 [--categorizer-cmd CMD] [--max-categories N] [--out FILE]
```

`<session>` is a Claude Code transcript (`.jsonl`) or an OpenCode export
(`opencode session export <id> > s.json`).

### overview

- **Session**: a file argument, else `--session` (a Claude Code UUID or an
  OpenCode `ses_…` id), else the session the command runs in. If both harness
  variables are set it refuses and asks for `--session`.
- **Live context only**: entries after the last compaction. The compaction is
  its own row: Claude Code's summary, or OpenCode's summary plus the recent
  messages it keeps verbatim (often the largest row).
- **Every live message is in exactly one row**, and the totals equal
  `ctxed inspect`. The categorizer sees a sample of a long session; an entry it
  did not see joins the topic of the nearest entry before it.
- **Status and pending** are the model's reading of the session: `done`,
  `in progress`, or `?` when it gave none.
- **If the model call fails**, the sizes are still printed, as one
  "Whole session" row, and the error goes to stderr.
- **Categorizer**: `--categorizer-cmd` or `CTXED_CATEGORIZER_CMD` (prompt on
  stdin, answer on stdout), else the harness's own CLI — `claude -p --model
  haiku` for Claude Code, `opencode run --model opencode-go/deepseek-v4-flash`
  for OpenCode. `CTXED_CATEGORIZER_MODEL` overrides the model.
  `CTXED_CLAUDE_BIN` / `CTXED_OPENCODE_BIN` point at the CLIs if they are not on
  PATH.

### inspect

```sh
$ ctxed inspect session.json
IDX  ROLE       KIND       TOKENS  PREVIEW
0    user       message    103     add retry to the upload client
1    assistant  tool-call  479     Let me look at the upload client first.
2    system     message    114     New skills are available…
TOTAL 3 entries · 696 tokens  (tokenizer: approximation · approximate)
```

Lists the live context entry by entry. `--json` emits the same fields as an
object.

### categorize

Groups the entries into 2–5 categories and writes an editable
`<name>.categories.json`. Model options: `--categorizer-cmd`, or
`--base-url/--api-key/--model` (fallbacks `OPENAI_BASE_URL`, `OPENAI_API_KEY`,
`CTXED_MODEL`).

## Token counts

Counts are estimates by default: one token per four runes, rounded up, labelled
`estimate`/`approximate`. Name a model or encoding for a real tokenizer:

```sh
ctxed inspect session.json --model gpt-4o
ctxed overview --tokenizer cl100k_base
```

## Pruning is parked

Earlier versions dropped topics from a live session (`drop`, `prune`,
`compact-instruction`, `ctxed opencode`, an OpenCode plugin, and prune skills).
Compaction in both harnesses could bring dropped content back, so that path is
parked: the code and its tests stay in the tree, but no command, skill or
plugin reaches it. See [`parked/`](parked/) and
`openspec/changes/context-overview`.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | success (also when only the topic names were unavailable) |
| 1 | runtime error (missing file or transcript, unrecognized format) |
| 2 | usage error (no session, ambiguous session, unsafe session id) |

## Development

```sh
go test ./...                                    # offline, deterministic
scripts/verify-functionally.sh --all             # real Claude Code + OpenCode sessions, cheap models
CTXED_TEST_TIKTOKEN=1 go test ./internal/tokenize/
```

Test fixtures under `testdata/` are trimmed, schema-faithful samples of a real
OpenCode export and a real Claude Code transcript.
