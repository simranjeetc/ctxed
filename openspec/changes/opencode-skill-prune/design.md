# Design

## Roles

| Part | Does | Never does |
|---|---|---|
| `ctxed opencode categorize` | export the session, pick the live part, sort it, save the listing | touch the session store |
| `ctxed opencode drop` | add the chosen categories' ids to the drop list | remove anything from the list |
| skill `ctxed-prune` | run the commands, ask the user (multi-select) | decide what to drop |
| plugin | remove listed ids from each request | post messages, call binaries |

## State file

One JSON file per session at `$CTXED_STATE_DIR/<session>.json`, default
`~/.local/state/ctxed/opencode/`. Both ctxed and the plugin resolve the same
default from the home directory, so a server and a shell with different
environments still agree. Written atomically (temp file + rename) because the
plugin reads it while ctxed writes.

```json
{
  "session": "ses_…",
  "dropped": { "ids": ["msg_…"], "toolCallIds": ["call_…"] },
  "listing": [ { "id": 1, "label": "…", "entryIds": ["msg_…"], "toolCallIds": [], "tokens": 1234 } ]
}
```

Session ids are validated (`ses_` + alphanumerics) before they become a path.

## What is sorted

`opencode session export` is run with stdout redirected to a temp file: piped,
it truncates large sessions (observed at ~3.9 MB). From the export, ctxed
keeps entries after the last `compaction` item (earlier ones are no longer
sent) and removes ids already on the drop list. The compaction item itself is
not offered: dropping it would drop the whole summary.

## Categorizer

`--categorizer-cmd`, else `$CTXED_CATEGORIZER_CMD`, else a built-in transport
that runs `opencode run --format json --model M` and keeps the text parts
(`$CTXED_CATEGORIZER_MODEL`, default `opencode-go/deepseek-v4-flash`). This
replaces the shell script's role for this path; the script stays for
`ctxed categorize`.

## Filtering

The plugin drops a message when its id is listed, or when it carries a tool
part whose call id is listed (a tool result can travel without an id). An id
that is listed but absent from a request is ignored; this is what made the old
design fail. Any error reading the file leaves the request unchanged.

## Risks

- OpenCode's own compaction may summarize from the full history and bring a
  dropped topic back into the summary. The suite checks this; if it happens,
  it is reported, not hidden.
- A message added after a prune is kept until the next prune, by design.
