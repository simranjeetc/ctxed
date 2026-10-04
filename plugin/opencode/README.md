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
2. You pick buckets: `/ctxed-prune 2,4`. The plugin records that selection for
   the session.
3. On every dispatch, OpenCode runs the plugin's `session.hook("context")`
   handler with the live message list. The plugin re-derives the dropped set
   over the **live** transcript (`ctxed prune - … --ids-only`), removes matching
   messages by id, and leaves everything else untouched. Stored history is never
   written.

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

Configuration is read from plugin options first, then from environment
variables.

| Option             | Environment variable              | Meaning                                                          |
| ------------------ | --------------------------------- | ---------------------------------------------------------------- |
| `ctxedPath`        | `CTXED_PLUGIN_CTXED_PATH`         | Path to the ctxed binary (default: `ctxed`, resolved on `PATH`). |
| `categorizerCmd`   | `CTXED_PLUGIN_CATEGORIZER_CMD`    | Command ctxed runs to categorize (prompt on its stdin).          |
| `categorizerModel` | `CTXED_PLUGIN_CATEGORIZER_MODEL`  | Model name, when the categorizer is a model endpoint.            |
| `maxCategories`    | `CTXED_PLUGIN_MAX_CATEGORIES`     | Maximum number of buckets to ask ctxed for.                      |
| `timeoutMs`        | `CTXED_PLUGIN_TIMEOUT_MS`         | Hard timeout per ctxed invocation (default: 2000).               |

Plugin options in `opencode.json`:

```jsonc
{
  "plugins": [
    {
      "package": "./.opencode/plugins/ctxed-prune.js",
      "options": {
        "ctxedPath": "/usr/local/bin/ctxed",
        "categorizerModel": "opencode-go/deepseek-v4-flash"
      }
    }
  ]
}
```

## Develop

```sh
npm test               # node --test: id filter, fail-open, caching, serialization, no policy
npm run loadcheck      # bundle + load a real OpenCode session; asserts the plugin loads
```

The tests exercise the id filter, fail-open behavior (non-zero exit, hang,
invalid JSON), caching, configuration, both live-message encodings, and the
absence of policy in the source. `loadcheck` is the mandatory live check: a
plugin can fail to load silently.
