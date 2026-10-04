# Plugin contract

How a harness plugin applies a ctxed prune. The plugin is deliberately trivial:
it asks ctxed for a pruned transcript and substitutes it, or — where it holds
live message objects — asks for the dropped id set and filters by id. All
categorization, selection, id resolution, and validity handling live in ctxed.

## What the plugin provides

- **The session source.** A path to the session document ctxed can read: a
  Claude Code `.jsonl` transcript, or an OpenCode `session export` JSON. ctxed
  never opens a harness database.
- **A prune selection.** Either:
  - a categories file plus the category ids to drop (`--categories-file F
    --categories 1,3`), or
  - explicit stable entry ids to drop (`--ids msg_a,msg_b`).

## What the plugin consumes

```sh
ctxed prune <session> --categories-file <file> --categories <ids>
```

- **stdout** — the pruned transcript, in the session's own shape (OpenCode export
  JSON, or Claude Code JSONL). The plugin substitutes this for the outbound
  messages.
- **stderr** — diagnostics, including any `adjustment: …` lines where ctxed
  dropped a dependent entry to keep the transcript structurally valid.
- **exit code** — 0 on success; non-zero on failure.

The plugin does not parse, reorder, or reason about entries. It parses the
transcript it is given and uses it.

### Id-only output

A plugin that holds live message objects — OpenCode's dispatch hook does — can
avoid serialising a transcript on every dispatch. With `--ids-only`, ctxed
resolves the selection and any orphans exactly as above, but prints only the
**dropped** ids:

```sh
ctxed prune session.json --categories-file session.categories.json --categories 1,3 --ids-only
# {"droppedIds":["msg_ab12","msg_cd34"]}
```

- **stdout** — a single JSON object whose `droppedIds` array is exactly the
  resolved drop set after orphan resolution. An empty selection yields `[]`.
- **stderr** — the same diagnostics as a normal prune, including `adjustment:`
  lines.
- **exit code** — unchanged.

The plugin removes the messages whose id is in `droppedIds`, preserving order
and every other message. Output is deterministic for a given session and
selection.

## Invariants the plugin can rely on

- The stored session is never modified, and the session id is unchanged.
- Output is deterministic for a given session and selection.
- The transcript never contains a tool result whose tool call was pruned; ctxed
  resolves and reports such cases instead of emitting an invalid transcript.

## Per-harness mapping

**Claude Code.** The session is the transcript file. On dispatch, run `ctxed
prune` against that file and replace the outbound message list with the retained
lines.

**OpenCode.** Sessions live in SQLite, so export once to JSON and run `ctxed
prune` against the export. OpenCode's v2 plugin API exposes
`session.hook("context")`, which runs as the agent-loop request is assembled and
lets a plugin edit `event.messages`. Ask ctxed for the dropped ids and filter the
live messages by id — ctxed never opens the database:

```ts
import { Plugin } from "@opencode/plugin"

export default Plugin.define({
  id: "ctxed.prune",
  async setup(ctx) {
    await ctx.session.hook("context", async (event) => {
      // ctxed prune <export> … --ids-only → {"droppedIds":[…]}
      const dropped = new Set(await droppedIdsFromCtxed())
      event.messages = event.messages.filter((message) => !dropped.has(message.id))
    })
  },
})
```

`plugin/opencode/` is the working implementation of this shape — fail-open,
cached per selection and session revision, with configuration from plugin
options or environment. See `plugin/opencode/README.md`.

## What the plugin must not do

- Implement categorization, selection, or prune policy.
- Mutate the harness's stored session.
- Contain harness-specific logic beyond parsing the transcript and substituting
  it.

If a plugin needs more than "call ctxed and substitute", the missing behavior
belongs in ctxed, so it stays shared across harnesses.

## Minimal shape

```text
onDispatch(session):
  transcript = exec("ctxed", "prune", session.path,
                    "--categories-file", session.categoriesFile,
                    "--categories", session.selectedCategoryIds)
  return substituteMessages(parse(transcript.stdout))
```

Nothing else is required; a new harness is this shape with a different
`parse`/`substituteMessages`.
