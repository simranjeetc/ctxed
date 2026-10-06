# ctxed — OpenCode dispatch plugin

Runs the whole ctxed prune workflow inside a running OpenCode session: it
categorizes the **live** conversation into topic buckets, lets you pick buckets
to drop, and removes their messages from every subsequent request. It is
non-destructive, fail-open, and holds no prune policy of its own — it asks ctxed
for the buckets and the dropped ids, and does what ctxed says.

## How it works

1. In-session, you run `/ctxed-prune`. The plugin reads the live conversation
   (`session.context`), hands it to ctxed on stdin, and prints the buckets:
   labels only — never message ids.
2. You pick buckets by answering with their numbers — just `3`, or `2,4`. A
   `prompt` hook matches that reply, applies the buckets already listed, and
   rewrites the message so the model sees what was dropped. `/ctxed-prune 2,4`
   still works, and reuses the same listing instead of categorizing again.
3. On every dispatch, OpenCode runs the plugin's `session.hook("context")`
   handler with the live message list. The plugin re-derives the dropped set
   over the **live** transcript (`ctxed prune - … --ids-only`), removes matching
   messages by id, and leaves everything else untouched. Stored history is never
   written.

The listing from step 1 stands for ten minutes: a message in between (a question,
a queued turn) does not discard it, so you can answer with the numbers whenever
you are ready. Only a number that names a bucket applies it — a number inside a
sentence never does.

Because the dropped set is re-derived on each dispatch, a message added after
you made the selection is covered by it too — the session does not drift out of
the selection.

Any ctxed failure — non-zero exit, timeout, or unparseable output — leaves the
transcript unchanged and logs the error. Pruning is an optimization; it never
blocks a turn.

The dropped set is cached per selection and per session revision (the ordered
message ids), so repeated dispatches do not re-invoke ctxed.

## Requirements

- OpenCode with the v2 plugin API (`ctx.session.hook`, `ctx.command.transform`).
- `ctxed` on `PATH`, or a configured path (see below).
- A categorizer for the `/ctxed-prune` step: a `--categorizer-cmd`, or a model.

## Build

OpenCode loads a local plugin as a **single flat file** under
`.opencode/plugins/` — it does not scan a subdirectory. The plugin is therefore
bundled before install:

```sh
npm install
npm run build          # esbuild → dist/ctxed-prune.js
```

`@opencode/plugin` stays external; OpenCode resolves it from the project's
`.opencode/node_modules` at load time.

## Install

```sh
# from your project root, with an opencode.json present
mkdir -p .opencode/plugins
cp <ctxed>/plugin/opencode/dist/ctxed-prune.js .opencode/plugins/ctxed-prune.js
```

Add the API dependency to the directory OpenCode installs from
(`.opencode/package.json`):

```json
{
  "dependencies": { "@opencode/plugin": "2.0.22" }
}
```

OpenCode discovers a newly added plugin asynchronously; `opencode plugin list`
may say "No plugins found" for a few seconds. A load failure is **silent** in a
normal run — check with `--print-logs --log-level debug`.

## Configure

The plugin works with **no configuration**: it resolves `ctxed` from the usual
install locations (because OpenCode's server runs with a minimal `PATH`) and
falls back to the categorizer script shipped beside it. Set the options below
only to override that.

Configuration is read from environment variables (`CTXED_PLUGIN_*`). A local
plugin in `.opencode/plugins/` cannot take options from `opencode.json` — only
npm plugins can — so environment variables are the channel.

| Environment variable                 | Meaning                                                          |
| ------------------------------------ | ---------------------------------------------------------------- |
| `CTXED_PLUGIN_CTXED_PATH`            | Path to the ctxed binary (default: resolved from common locations). |
| `CTXED_PLUGIN_CATEGORIZER_CMD`       | Command ctxed runs to categorize (prompt on its stdin).          |
| `CTXED_PLUGIN_CATEGORIZER_MODEL`     | Model name, when the categorizer is a model endpoint.            |
| `CTXED_PLUGIN_MAX_CATEGORIES`        | Maximum number of buckets to ask ctxed for.                      |
| `CTXED_PLUGIN_TIMEOUT_MS`            | Hard timeout for the dispatch prune (default: 2000).             |
| `CTXED_PLUGIN_COMMAND_TIMEOUT_MS`    | Timeout for the command's model-backed categorize (default: 60000). |
| `CTXED_PLUGIN_DEBUG_LOG`             | When set, append each dispatch decision (kept/dropped ids) here. |

**Output is a session message.** A command's `execute` returns void, so the
plugin surfaces the bucket list and the selection confirmation as synthetic
session messages — they appear in the chat. `console.log` would go to the
server's stdout, which the user never sees.

## Develop

```sh
npm test               # node --test: id filter, fail-open, caching, serialization, no policy
npm run loadcheck      # bundle + load a real OpenCode session; asserts the plugin loads
```

The tests exercise the id filter, fail-open behavior (non-zero exit, hang,
invalid JSON), caching, configuration, both live-message encodings, and the
absence of policy in the source. `loadcheck` is the mandatory live check: a
plugin can fail to load silently.
