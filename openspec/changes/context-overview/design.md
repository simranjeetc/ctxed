## Context

ctxed already reads both harnesses' sessions, finds the live context of a
compacted Claude Code transcript, finds the live part of an OpenCode export, and
asks a model to group entries into labelled categories with token counts. The
overview reuses all of it and changes nothing on disk except ctxed's own temp files.

## Decisions

### D1. One command, two harness inputs

`ctxed overview [<file>] [--session ID] [--json]`:

| Input | Claude Code | OpenCode |
|---|---|---|
| Session from env | `CLAUDE_CODE_SESSION_ID` → `<id>.jsonl` in the cwd's project folder, else any folder under `$CLAUDE_CONFIG_DIR/projects` (default `~/.claude`) | `OPENCODE_SESSION_ID` → `opencode session export` to a temp file |
| Live context | After the last `compact_boundary` (existing Claude adapter) | The last `compaction` item and what follows it (the OpenCode adapter now does this, like the Claude one) |

A file argument overrides both; `--session` takes either kind of id. With
neither, it exits 2 and says how to pass one. If both variables are set (one
harness launched from the other), it exits 2 and asks for `--session`.

### D2. What the report shows

```
Session <id> · live context: N messages · ~T tokens (estimate)

 #  Topic                     Msgs  Tokens  Share  Status
 1  ...                         78  127.9k    44%  done
 2  ...                         57   73.6k    25%  in progress
    Compaction summary           1    9.1k     3%  —

Pending: <item>; <item>
Not counted: K messages from before the last compaction.
```

- Topic rows are sorted by tokens, largest first.
- The compaction summary (and, for OpenCode, the kept verbatim tail inside the
  compaction record) is its own row, never folded into a topic, so totals are
  complete.
- Totals equal the sum of rows and equal `ctxed inspect` over the same live view.

### D2b. Entries the categorizer did not see

The prompt samples at most 250 entries of a long session. An entry left out
joins the topic of the nearest earlier placed entry (else the nearest later
one), so every live entry is in exactly one row and totals stay complete. If
the model call fails, the sizes still print as one "Whole session" row and the
error goes to stderr (exit 0).

### D3. Status and pending come from the categorizing call

The existing prompt gains two fields: `"status": "done" | "in_progress"` per
category, and a top-level `"pending": ["..."]` (at most 5 short items). A missing
or unknown status prints as `?`; a missing `pending` prints nothing. The
categorizer transport is unchanged: `--categorizer-cmd`, `CTXED_CATEGORIZER_CMD`,
else the harness's own CLI (`claude -p` with Haiku / `opencode run`).

### D4. Token counts are estimates

The existing approximate tokenizer (about 4 characters per token) is used for
both harnesses, and the header says "estimate".

### D5. Parking pruning

Code under `internal/ocprune`, `internal/prune`, `internal/compact`, the plugin and the prune skills
stays in the repo but is unreachable: the CLI cases are deleted and the usage
text no longer lists them. The parked CLI tests run through `cli.RunWithParked`,
defined in `export_test.go`, which only test builds compile. The OpenCode
export, categorizer transport and session-id validation move to
`internal/harness`, so nothing live imports `ocprune`. The Claude Code prune
skill moves to `parked/claude-skill/`, the old live script to
`parked/verify-prune-functionally.sh`, and the global OpenCode plugin and
`ctxed-prune` skill are uninstalled.

### D6. One skill file

`skills/ctxed-overview/SKILL.md` is installed for both harnesses (symlinked into
`~/.claude/skills/`, copied into `~/.config/opencode/skills/`). It runs
`ctxed overview` and shows the output as printed; it runs nothing else.

## Risks

- The Claude transcript is written asynchronously; the last turn may be missing
  from the report. The skill says so in one line.
- Model-chosen status can be wrong; it is a hint, labelled as such in `--help`.
